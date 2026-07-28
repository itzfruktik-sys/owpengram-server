#!/usr/bin/env python3
"""Interactive TUI control panel for the OwpenGram server.

Wraps the same operations as start-server.sh / start-server.bat (Docker
naming migration, Postgres naming migration, docker compose up, Go build,
launching owpengram-server / owpengram-admin-panel) behind a menu you can
navigate instead of re-running a script from scratch each time.

Starting the server launches both binaries as fully detached background
processes and writes their PIDs to .server_panel.json next to .env; closing
the panel (Exit) does NOT stop them -- only the Stop action does. Reopening
the panel later picks the same PIDs back up and reports live status.

Requires: pip install textual
Run: python server-panel.py
"""
from __future__ import annotations

import json
import os
import platform
import re
import shutil
import signal
import subprocess
import time
from dataclasses import dataclass, field
from pathlib import Path

import psutil
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat, load_pem_private_key
from textual import work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical, VerticalScroll
from textual.screen import Screen
from textual.widgets import Button, Collapsible, DataTable, Footer, Header, Input, Label, OptionList, ProgressBar, RichLog, Static
from textual.widgets.option_list import Option

IS_WINDOWS = platform.system() == "Windows"

# psutil.cpu_percent()'s first call always returns a meaningless 0.0 baseline
# (it measures against process start); priming it once here means the first
# real reading in the stats timer is already a proper since-last-call delta.
psutil.cpu_percent(interval=None)

ROOT = Path(__file__).resolve().parent
DEPLOY_DIR = ROOT / "deploy"
BIN_DIR = ROOT / "bin"
LOG_DIR = ROOT / "logs"
ENV_FILE = ROOT / ".env"
ENV_EXAMPLE_FILE = ROOT / ".env.example"
COMPOSE_FILE = DEPLOY_DIR / "docker-compose.yml"
STATE_FILE = ROOT / ".server_panel.json"

SERVER_EXE = BIN_DIR / ("owpengram-server.exe" if IS_WINDOWS else "owpengram-server")
ADMIN_EXE = BIN_DIR / ("owpengram-admin-panel.exe" if IS_WINDOWS else "owpengram-admin-panel")
SERVER_LOG = LOG_DIR / "owpengram-server.log"
ADMIN_LOG = LOG_DIR / "owpengram-admin-panel.log"

BANNER = r"""
   _____      _____ ___ _  _  ___ ___    _   __  __
  / _ \ \    / / _ \ __| \| |/ __| _ \  /_\ |  \/  |
 | (_) \ \/\/ /|  _/ _|| .` | (_ |   / / _ \| |\/| |
  \___/ \_/\_/ |_| |___|_|\_|\___|_|_\/_/ \_\_|  |_|
""".strip("\n")


# --- Process/state management -----------------------------------------------


def load_state() -> dict:
    if STATE_FILE.exists():
        try:
            return json.loads(STATE_FILE.read_text())
        except (OSError, json.JSONDecodeError):
            return {}
    return {}


def save_state(state: dict) -> None:
    STATE_FILE.write_text(json.dumps(state))


def pid_alive(pid: int | None) -> bool:
    if not pid:
        return False
    if IS_WINDOWS:
        out = subprocess.run(
            ["tasklist", "/FI", f"PID eq {pid}"],
            capture_output=True, text=True,
        ).stdout
        return str(pid) in out
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    return True


def kill_pid(pid: int | None) -> None:
    if not pid:
        return
    if IS_WINDOWS:
        # /T kills the whole process tree, not just this PID -- matters when
        # the process was launched via a shell wrapper.
        subprocess.run(["taskkill", "/PID", str(pid), "/T", "/F"], capture_output=True)
        return
    try:
        os.killpg(pid, signal.SIGTERM)
    except OSError:
        try:
            os.kill(pid, signal.SIGTERM)
        except OSError:
            pass
    time.sleep(1)
    try:
        os.killpg(pid, signal.SIGKILL)
    except OSError:
        try:
            os.kill(pid, signal.SIGKILL)
        except OSError:
            pass


@dataclass
class Status:
    server_pid: int | None
    server_alive: bool
    admin_pid: int | None
    admin_alive: bool
    containers: list[tuple[str, str | None]]

    @property
    def running(self) -> bool:
        return self.server_alive or self.admin_alive


class ServerManager:
    """Owns the actual start/stop/build/launch mechanics. No Textual
    dependency in here on purpose, so it stays easy to reason about /
    reuse outside the TUI if that's ever useful."""

    def container_status(self, name: str) -> str | None:
        """Returns Docker's own State.Status (running/exited/created/...),
        or None if the container doesn't exist at all."""
        r = subprocess.run(
            ["docker", "inspect", "-f", "{{.State.Status}}", name],
            capture_output=True, text=True,
        )
        if r.returncode != 0:
            return None
        return r.stdout.strip() or None

    def status(self) -> Status:
        state = load_state()
        server_pid = state.get("server_pid")
        admin_pid = state.get("admin_pid")
        # Falls back to the cached naming decision (or the "owpengram"
        # default for a never-started, fresh install) so the container list
        # shows something sensible even before Start has ever run.
        prefix = self.cached_docker_naming() or state.get("docker_prefix") or "owpengram"
        containers = [
            (f"{prefix}-{service}", self.container_status(f"{prefix}-{service}"))
            for service in ("postgres", "redis")
        ]
        return Status(
            server_pid=server_pid,
            server_alive=pid_alive(server_pid),
            admin_pid=admin_pid,
            admin_alive=pid_alive(admin_pid),
            containers=containers,
        )

    # -- naming migrations (interactive, must run with the real terminal) --
    #
    # Both migrations cache their decision in a plain state file the first
    # time they run. Reading that file directly and skipping the subprocess
    # (and the app.suspend()/resume dance it needs) whenever a decision is
    # already cached keeps suspend() out of the hot path for every ordinary
    # Start/Restart -- suspending and resuming Textual's terminal driver
    # repeatedly is what was leaving the menu unresponsive until the app was
    # fully restarted. It's now only exercised once per machine, the first
    # time there's an actual prompt to show.

    def cached_docker_naming(self) -> str | None:
        state_file = ROOT / ".docker_naming"
        if not state_file.exists():
            return None
        value = state_file.read_text().strip()
        return value if value in ("owpengram", "telesrv") else None

    def cached_db_naming(self) -> str | None:
        state_file = ROOT / ".db_naming"
        if not state_file.exists():
            return None
        value = state_file.read_text().strip()
        return value if value in ("owpengram", "telesrv") else None

    def resolve_docker_naming(self, run_interactive) -> tuple[str, str]:
        if IS_WINDOWS:
            cmd = [
                "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
                "-File", str(DEPLOY_DIR / "migrate-docker-naming.ps1"),
            ]
        else:
            cmd = [
                "bash", str(DEPLOY_DIR / "migrate-docker-naming.sh"),
                str(COMPOSE_FILE), str(ROOT / ".docker_naming"),
            ]
        out = run_interactive(cmd)
        lines = [l.strip() for l in out.splitlines() if l.strip()]
        if len(lines) >= 2:
            return lines[0], lines[1]
        return "owpengram", "owpengram"

    def resolve_db_naming(self, run_interactive, container_name: str) -> None:
        if IS_WINDOWS:
            cmd = [
                "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
                "-File", str(DEPLOY_DIR / "migrate-db-naming.ps1"),
                "-ContainerName", container_name,
            ]
        else:
            cmd = [
                "bash", str(DEPLOY_DIR / "migrate-db-naming.sh"),
                container_name, str(ENV_FILE), str(ROOT / ".db_naming"),
            ]
        run_interactive(cmd)

    # -- non-interactive steps (safe to run off the UI thread) --

    def docker_compose_up(self, project: str, prefix: str) -> tuple[bool, str]:
        env = os.environ.copy()
        env["TELESRV_DOCKER_PROJECT"] = project
        env["TELESRV_DOCKER_PREFIX"] = prefix
        r = subprocess.run(
            ["docker", "compose", "-f", str(COMPOSE_FILE), "up", "-d"],
            cwd=ROOT, env=env, capture_output=True, text=True,
        )
        return r.returncode == 0, (r.stdout + r.stderr)

    def docker_compose_stop(self, project: str, prefix: str) -> None:
        env = os.environ.copy()
        env["TELESRV_DOCKER_PROJECT"] = project
        env["TELESRV_DOCKER_PREFIX"] = prefix
        subprocess.run(
            ["docker", "compose", "-f", str(COMPOSE_FILE), "stop"],
            cwd=ROOT, env=env, capture_output=True,
        )

    def wait_postgres(self, prefix: str, timeout: float = 60) -> bool:
        deadline = time.time() + timeout
        while time.time() < deadline:
            r = subprocess.run(
                ["docker", "exec", f"{prefix}-postgres", "pg_isready", "-U", "telesrv", "-d", "telesrv"],
                capture_output=True,
            )
            if r.returncode == 0:
                return True
            time.sleep(2)
        return False

    def build(self) -> tuple[bool, str]:
        BIN_DIR.mkdir(exist_ok=True)
        log = []
        for out_path, pkg in ((SERVER_EXE, "./cmd/telesrv"), (ADMIN_EXE, "./cmd/telesrv-admin")):
            r = subprocess.run(
                ["go", "build", "-o", str(out_path), pkg],
                cwd=ROOT, capture_output=True, text=True,
            )
            log.append(f"$ go build -o {out_path.name} {pkg}\n{r.stdout}{r.stderr}")
            if r.returncode != 0:
                return False, "\n".join(log)
        return True, "\n".join(log)

    def launch(self, exe_path: Path, log_path: Path) -> int:
        LOG_DIR.mkdir(exist_ok=True)
        logf = open(log_path, "ab")
        kwargs: dict = {}
        if IS_WINDOWS:
            kwargs["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP | subprocess.DETACHED_PROCESS
        else:
            kwargs["start_new_session"] = True
        proc = subprocess.Popen(
            [str(exe_path)], cwd=ROOT,
            stdout=logf, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL,
            **kwargs,
        )
        logf.close()
        return proc.pid

    def stop(self) -> None:
        state = load_state()
        kill_pid(state.get("server_pid"))
        kill_pid(state.get("admin_pid"))
        project = state.get("docker_project", "owpengram")
        prefix = state.get("docker_prefix", "owpengram")
        self.docker_compose_stop(project, prefix)
        state["server_pid"] = None
        state["admin_pid"] = None
        save_state(state)


MANAGER = ServerManager()


# --- .env / clipboard / system stats helpers --------------------------------


def read_env_value(key: str) -> str | None:
    if not ENV_FILE.exists():
        return None
    prefix = f"{key}="
    for line in ENV_FILE.read_text(encoding="utf-8", errors="replace").splitlines():
        line = line.strip()
        if line.startswith(prefix):
            return line[len(prefix):].strip()
    return None


# --- .env.example parsing / editing ------------------------------------

_ACTIVE_FIELD_RE = re.compile(r"^(TELESRV_[A-Z0-9_]+)=(.*)$")
_COMMENTED_FIELD_RE = re.compile(r"^#\s*(TELESRV_[A-Z0-9_]+)=(.*)$")
_SENSITIVE_KEY_RE = re.compile(r"(PASSWORD|SECRET|_TOKEN|API_KEY)")


@dataclass
class EnvField:
    key: str
    default_value: str
    description: str
    enabled_by_default: bool

    @property
    def sensitive(self) -> bool:
        return bool(_SENSITIVE_KEY_RE.search(self.key))


@dataclass
class EnvGroup:
    title: str
    fields: list[EnvField] = field(default_factory=list)


def parse_env_template() -> list[EnvGroup]:
    """Parses .env.example into groups of fields for the editor. Grouping
    follows the file's own blank-line-separated blocks; a group's title is
    the first sentence of whichever field in it has a comment (falling back
    to its first key when none of them do, e.g. the bare Postgres/Redis DSN
    pair that has no comment of its own)."""
    if not ENV_EXAMPLE_FILE.exists():
        return []

    groups: list[EnvGroup] = []
    current_fields: list[EnvField] = []
    pending: list[str] = []
    in_comment_run = False
    seen_keys: set[str] = set()

    def flush_group() -> None:
        nonlocal current_fields
        if current_fields:
            described = next((f.description for f in current_fields if f.description), "")
            if described:
                title = described.split(". ", 1)[0].strip().rstrip(".")
            else:
                title = current_fields[0].key
            groups.append(EnvGroup(title=title[:70], fields=current_fields))
        current_fields = []

    for raw_line in ENV_EXAMPLE_FILE.read_text(encoding="utf-8", errors="replace").splitlines():
        stripped = raw_line.strip()

        if not stripped:
            flush_group()
            pending = []
            in_comment_run = False
            continue

        active = _ACTIVE_FIELD_RE.match(stripped)
        if active:
            # TELESRV_AI_PROVIDERS legitimately appears twice in the file: once
            # as the real setting, once as an inline "if you want Kimi" example
            # inside an unrelated comment block further down. A second Input
            # for the same env var would just be confusing, so the first
            # occurrence wins and later repeats fold into descriptive text.
            if active.group(1) not in seen_keys:
                seen_keys.add(active.group(1))
                current_fields.append(EnvField(
                    key=active.group(1), default_value=active.group(2),
                    description=" ".join(pending), enabled_by_default=True,
                ))
            in_comment_run = False
            continue

        if stripped.startswith("#"):
            commented = _COMMENTED_FIELD_RE.match(stripped)
            if commented and commented.group(1) not in seen_keys:
                seen_keys.add(commented.group(1))
                current_fields.append(EnvField(
                    key=commented.group(1), default_value=commented.group(2),
                    description=" ".join(pending), enabled_by_default=False,
                ))
                in_comment_run = False
                continue
            text = stripped.lstrip("#").strip()
            if in_comment_run:
                pending.append(text)
            else:
                pending = [text]
            in_comment_run = True
            continue

        # Any other line (shouldn't normally happen in this file) just ends
        # whatever comment run was in progress without touching fields.
        in_comment_run = False

    flush_group()
    return groups


def current_env_values(groups: list[EnvGroup]) -> dict[str, str]:
    """Current value for every known field: from .env if it's set there,
    otherwise the template's default -- EXCEPT for a field that's commented
    out (disabled) in the template, whose "default_value" is only the
    example shown in the comment (e.g. "openai_responses"), not something
    that should silently start populated and get activated on save. Those
    start blank; the example still shows as the input's placeholder."""
    values: dict[str, str] = {}
    for group in groups:
        for f in group.fields:
            existing = read_env_value(f.key)
            if existing is not None:
                values[f.key] = existing
            elif f.enabled_by_default:
                values[f.key] = f.default_value
            else:
                values[f.key] = ""
    return values


def save_env(values: dict[str, str]) -> None:
    """(Re)writes .env from .env.example's exact text, substituting each
    known field's value in place -- this is what preserves every comment and
    the file's layout untouched. A template-commented optional field is
    uncommented when given a non-empty value, and left as-is (commented,
    disabled) when left empty."""
    out_lines = []
    seen_keys: set[str] = set()
    for raw_line in ENV_EXAMPLE_FILE.read_text(encoding="utf-8", errors="replace").splitlines():
        stripped = raw_line.strip()
        active = _ACTIVE_FIELD_RE.match(stripped)
        if active and active.group(1) in values and active.group(1) not in seen_keys:
            seen_keys.add(active.group(1))
            out_lines.append(f"{active.group(1)}={values[active.group(1)]}")
            continue
        commented = _COMMENTED_FIELD_RE.match(stripped)
        if commented and commented.group(1) in values and commented.group(1) not in seen_keys:
            seen_keys.add(commented.group(1))
            key = commented.group(1)
            value = values[key]
            out_lines.append(f"{key}={value}" if value else raw_line)
            continue
        out_lines.append(raw_line)
    ENV_FILE.write_text("\n".join(out_lines) + "\n", encoding="utf-8")


def admin_ui_info() -> tuple[str, str | None] | None:
    """Returns (url, password) for the admin UI, or None if it isn't
    configured at all. password is None when TELESRV_ADMIN_UI_PASSWORD is
    empty (token-only auth)."""
    addr = read_env_value("TELESRV_ADMIN_UI_ADDR")
    if not addr:
        return None
    url = addr if addr.startswith(("http://", "https://")) else f"http://{addr}"
    return url, (read_env_value("TELESRV_ADMIN_UI_PASSWORD") or None)


def server_address() -> str | None:
    """The MTProto address clients connect to (advertise IP + listen port).
    Present as soon as .env is configured, even before the first Start."""
    ip = read_env_value("TELESRV_ADVERTISE_IP")
    listen = read_env_value("TELESRV_LISTEN")
    if not ip or not listen:
        return None
    port = listen.rsplit(":", 1)[-1]
    if not port.isdigit():
        return None
    return f"{ip}:{port}"


def server_public_key_pem() -> str | None:
    """The server's RSA public key (PKCS1 "RSA PUBLIC KEY" PEM, the format
    patched into client builds), derived from the private key file. That
    file is only generated the first time telesrv actually starts, so this
    is legitimately absent on a never-started install -- and returns None
    rather than raising on a missing/corrupt/unparseable file instead of
    crashing the panel over it."""
    key_path_value = read_env_value("TELESRV_RSA_KEY") or "data/server_rsa.pem"
    key_path = Path(key_path_value)
    if not key_path.is_absolute():
        key_path = ROOT / key_path
    if not key_path.exists():
        return None
    try:
        private_key = load_pem_private_key(key_path.read_bytes(), password=None)
        public_pem = private_key.public_key().public_bytes(Encoding.PEM, PublicFormat.PKCS1)
        return public_pem.decode("ascii").strip()
    except Exception:  # noqa: BLE001 - any parse/format issue just means "unavailable"
        return None


def copy_to_clipboard(text: str) -> bool:
    if IS_WINDOWS:
        candidates = [["clip"]]
    elif platform.system() == "Darwin":
        candidates = [["pbcopy"]]
    else:
        candidates = [
            ["xclip", "-selection", "clipboard"],
            ["xsel", "--clipboard", "--input"],
            ["wl-copy"],
        ]
    for cmd in candidates:
        try:
            subprocess.run(cmd, input=text.encode("utf-8"), check=True, capture_output=True)
            return True
        except (FileNotFoundError, subprocess.CalledProcessError):
            continue
    return False


def system_stats() -> tuple[float, float, float]:
    """Returns (cpu_percent, ram_percent, disk_percent) for the drive this
    project lives on."""
    cpu = psutil.cpu_percent(interval=None)
    ram = psutil.virtual_memory().percent
    disk = psutil.disk_usage(str(ROOT)).percent
    return cpu, ram, disk


# --- UI -----------------------------------------------------------------


class CopyButton(Static):
    """A clickable label that copies `value` to the clipboard on click. The
    actual value is never shown on screen -- just the label and a "click to
    copy" hint (optionally masked with dots for secret-looking fields, purely
    cosmetic since nothing is ever displayed either way). value=None renders
    as "not available" and isn't clickable -- used before the first Start,
    when e.g. the RSA public key file doesn't exist yet."""

    def __init__(self, label: str, value: str | None, on_copy_callback, *, masked: bool = False, **kwargs):
        super().__init__(**kwargs)
        self.label = label
        self.value = value
        self.masked = masked
        self._on_copy_callback = on_copy_callback

    def on_mount(self) -> None:
        self._update_display()

    def _update_display(self) -> None:
        # NOTE: deliberately not named `_render` -- that name collides with
        # Widget's own internal `_render()` (returns a Visual, used by
        # get_content_height during layout); shadowing it with a method that
        # returns None broke height calculation as soon as anything forced a
        # reflow (e.g. pushing another screen), with a `'NoneType' object has
        # no attribute 'get_height'` crash.
        if self.value is None:
            self.update(f"{self.label}: [dim]not available[/]")
        elif self.masked:
            self.update(f"{self.label}: [dim]{'•' * 10}[/]  [i](click to copy)[/]")
        else:
            self.update(f"{self.label}: [dim](click to copy)[/]")

    def set_value(self, value: str | None) -> None:
        """Updates the underlying value (e.g. after the env editor changes
        it) without ever having displayed the old one on screen."""
        self.value = value
        self._update_display()

    def on_click(self, event) -> None:
        if self.value is not None:
            self._on_copy_callback(self)


class LogTailScreen(Screen):
    """Live-tails one or two log files. Escape/b goes back to the main menu."""

    BINDINGS = [
        Binding("escape", "back", "Back"),
        Binding("b", "back", "Back"),
    ]

    def __init__(self, panes: list[tuple[str, Path]]):
        super().__init__()
        self._panes = panes
        self._offsets: dict[Path, int] = {path: 0 for _, path in panes}
        self._logs: dict[Path, RichLog] = {}

    def compose(self) -> ComposeResult:
        yield Header()
        with Horizontal():
            for title, path in self._panes:
                with Vertical():
                    yield Label(f" {title} ({path.name}) ")
                    log = RichLog(highlight=False, markup=False, wrap=True)
                    self._logs[path] = log
                    yield log
        yield Footer()

    def on_mount(self) -> None:
        for title, path in self._panes:
            self._prime(path)
        self.set_interval(0.5, self._poll)

    def _prime(self, path: Path) -> None:
        # Seed each pane with the tail of the file instead of starting empty.
        if not path.exists():
            self._logs[path].write("(no logs yet)")
            return
        size = path.stat().st_size
        with open(path, "rb") as f:
            f.seek(max(0, size - 8000))
            data = f.read()
        self._offsets[path] = size
        text = data.decode("utf-8", errors="replace")
        if text:
            self._logs[path].write(text)

    def _poll(self) -> None:
        for _, path in self._panes:
            if not path.exists():
                continue
            size = path.stat().st_size
            offset = self._offsets[path]
            if size < offset:
                # Log file got rotated/truncated (e.g. new run started).
                offset = 0
            if size > offset:
                with open(path, "rb") as f:
                    f.seek(offset)
                    data = f.read()
                self._offsets[path] = size
                self._logs[path].write(data.decode("utf-8", errors="replace"))

    def action_back(self) -> None:
        self.app.pop_screen()


class LogPickerScreen(Screen):
    BINDINGS = [Binding("escape", "back", "Back")]

    def compose(self) -> ComposeResult:
        yield Header()
        yield OptionList(
            Option("owpengram-server logs", id="server"),
            Option("owpengram-admin-panel logs", id="admin"),
            Option("Both (split view)", id="both"),
            Option("Back", id="back"),
        )
        yield Footer()

    def action_back(self) -> None:
        self.app.pop_screen()

    def on_option_list_option_selected(self, event: OptionList.OptionSelected) -> None:
        option_id = event.option.id
        if option_id == "server":
            self.app.push_screen(LogTailScreen([("owpengram-server", SERVER_LOG)]))
        elif option_id == "admin":
            self.app.push_screen(LogTailScreen([("owpengram-admin-panel", ADMIN_LOG)]))
        elif option_id == "both":
            self.app.push_screen(LogTailScreen([
                ("owpengram-server", SERVER_LOG),
                ("owpengram-admin-panel", ADMIN_LOG),
            ]))
        else:
            self.app.pop_screen()


class EnvEditorScreen(Screen):
    """Every field .env.example knows about, grouped and described, with an
    Input per field. If .env doesn't exist yet, it's copied verbatim from
    .env.example first (so editing starts from the same defaults a fresh
    `cp .env.example .env` would have given you). Save rewrites .env from
    .env.example's exact text with just the values swapped in, so every
    comment and the file's layout survive untouched; Back discards unsaved
    edits."""

    BINDINGS = [
        Binding("escape", "back", "Back"),
        Binding("ctrl+s", "save", "Save"),
    ]

    def __init__(self) -> None:
        super().__init__()
        self._created_env = False
        if not ENV_FILE.exists() and ENV_EXAMPLE_FILE.exists():
            shutil.copy(ENV_EXAMPLE_FILE, ENV_FILE)
            self._created_env = True
        self._groups: list[EnvGroup] = parse_env_template()
        self._values: dict[str, str] = current_env_values(self._groups)

    def compose(self) -> ComposeResult:
        yield Header()
        yield Static("Environment configuration (.env)", id="env-title")
        if not self._groups:
            yield Static(
                "[b red]No .env.example found -- nothing to configure.[/]",
                id="env-missing",
            )
        else:
            with VerticalScroll(id="env-scroll"):
                for i, group in enumerate(self._groups):
                    with Collapsible(title=f"{group.title} ({len(group.fields)})", id=f"env-group-{i}"):
                        for f in group.fields:
                            with Vertical(classes="env-field"):
                                badge = "" if f.enabled_by_default else "  [dim i](optional, currently disabled)[/]"
                                yield Static(f"[b]{f.key}[/]{badge}", classes="env-field-key")
                                if f.description:
                                    yield Static(f.description, classes="env-field-desc")
                                yield Input(
                                    value=self._values.get(f.key, ""),
                                    placeholder=f.default_value if not f.enabled_by_default else "",
                                    password=f.sensitive,
                                    id=self._input_id(f.key),
                                )
            with Horizontal(id="env-actions"):
                yield Button("Save", id="env-save-button", variant="success")
                yield Button("Back", id="env-back-button", variant="default")
        yield Footer()

    @staticmethod
    def _input_id(key: str) -> str:
        return f"env-{key}"

    def on_mount(self) -> None:
        if self._created_env:
            self.notify("Created .env from .env.example")

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "env-save-button":
            self.action_save()
        elif event.button.id == "env-back-button":
            self.action_back()

    def action_save(self) -> None:
        if not self._groups:
            return
        values = dict(self._values)
        for group in self._groups:
            for f in group.fields:
                values[f.key] = self.query_one(f"#{self._input_id(f.key)}", Input).value
        try:
            save_env(values)
        except OSError as exc:
            self.notify(f"Failed to save .env: {exc}", severity="error", timeout=10)
            return
        self._values = values
        self.notify(".env saved")

    def action_back(self) -> None:
        self.dismiss()


class MainScreen(Screen):
    BINDINGS = [
        Binding("1", "start", "Start"),
        Binding("2", "stop", "Stop"),
        Binding("3", "restart", "Restart"),
        Binding("4", "logs", "Logs"),
        Binding("5", "env", "Configure .env"),
        Binding("q", "quit_panel", "Exit"),
    ]

    def compose(self) -> ComposeResult:
        yield Header()
        yield Static(BANNER, id="banner")
        with Horizontal(id="main-body"):
            with Vertical(id="services-panel"):
                yield Label("Services", classes="panel-title")
                yield DataTable(id="services-table", cursor_type="none")
            with Vertical(id="sidebar"):
                yield Label("System", classes="panel-title")
                yield Label("CPU", id="cpu-label", classes="stat-label")
                yield ProgressBar(total=100, id="cpu-bar", show_eta=False)
                yield Label("RAM", id="ram-label", classes="stat-label")
                yield ProgressBar(total=100, id="ram-bar", show_eta=False)
                yield Label("Disk", id="disk-label", classes="stat-label")
                yield ProgressBar(total=100, id="disk-bar", show_eta=False)
                yield Label("Server", classes="panel-title")
                yield CopyButton("Address", server_address(), self._handle_copy_click, id="server-address")
                yield CopyButton("Public key", server_public_key_pem(), self._handle_copy_click, masked=True, id="server-public-key")
                yield Label("Admin UI", classes="panel-title")
                yield Static(id="admin-link")
                yield self._admin_password_widget()
        yield OptionList(
            Option("Start server", id="start"),
            Option("Stop server", id="stop"),
            Option("Restart server", id="restart"),
            Option("View logs", id="logs"),
            Option("Configure .env", id="env"),
            Option("Exit panel (server keeps running)", id="exit"),
        )
        yield Footer()

    def _admin_password_widget(self) -> CopyButton:
        info = admin_ui_info()
        password = info[1] if info else None
        return CopyButton("Password", password, self._handle_copy_click, masked=True, id="admin-password")

    def _handle_copy_click(self, widget: CopyButton) -> None:
        if copy_to_clipboard(widget.value):
            self.notify(f"{widget.label} copied to clipboard")
        else:
            self.notify(f"{widget.label} copy failed", severity="warning")

    def on_mount(self) -> None:
        self.query_one("#services-table", DataTable).add_columns("Service", "Type", "Status")
        self.refresh_status()
        self.refresh_stats()
        self.refresh_config_widgets()
        self.set_interval(3, self.refresh_status)
        self.set_interval(2, self.refresh_stats)
        self.set_interval(3, self.refresh_config_widgets)

    def refresh_status(self) -> None:
        status = MANAGER.status()
        table = self.query_one("#services-table", DataTable)
        table.clear()

        table.add_row(
            "owpengram-server", "binary",
            "[b green]● RUNNING[/]" if status.server_alive else "[dim]○ stopped[/]",
        )
        table.add_row(
            "owpengram-admin-panel", "binary",
            "[b green]● RUNNING[/]" if status.admin_alive else "[dim]○ stopped[/]",
        )
        for name, state in status.containers:
            if state == "running":
                cell = "[b green]● RUNNING[/]"
            elif state is None:
                cell = "[dim]○ not created[/]"
            else:
                cell = f"[b red]● {state.upper()}[/]"
            table.add_row(name, "container", cell)

    def refresh_stats(self) -> None:
        cpu, ram, disk = system_stats()
        self.query_one("#cpu-bar", ProgressBar).update(progress=cpu)
        self.query_one("#ram-bar", ProgressBar).update(progress=ram)
        self.query_one("#disk-bar", ProgressBar).update(progress=disk)
        self.query_one("#cpu-label", Label).update(f"CPU   {cpu:5.1f}%")
        self.query_one("#ram-label", Label).update(f"RAM   {ram:5.1f}%")
        self.query_one("#disk-label", Label).update(f"Disk  {disk:5.1f}%")

    def refresh_admin_link(self) -> None:
        info = admin_ui_info()
        widget = self.query_one("#admin-link", Static)
        if info:
            url, _ = info
            widget.update(f'[link="{url}"]{url}[/link]')
        else:
            widget.update("[dim]not configured[/]")

    def refresh_config_widgets(self) -> None:
        """Re-reads .env-derived values into the already-mounted widgets --
        matters after coming back from the env editor, since e.g. the admin
        password or the server's advertise address may have just changed."""
        self.refresh_admin_link()
        info = admin_ui_info()
        self.query_one("#admin-password", CopyButton).set_value(info[1] if info else None)
        self.query_one("#server-address", CopyButton).set_value(server_address())
        self.query_one("#server-public-key", CopyButton).set_value(server_public_key_pem())

    def on_option_list_option_selected(self, event: OptionList.OptionSelected) -> None:
        option_id = event.option.id
        if option_id == "start":
            self.action_start()
        elif option_id == "stop":
            self.action_stop()
        elif option_id == "restart":
            self.action_restart()
        elif option_id == "logs":
            self.action_logs()
        elif option_id == "env":
            self.action_env()
        elif option_id == "exit":
            self.action_quit_panel()

    def action_logs(self) -> None:
        self.app.push_screen(LogPickerScreen())

    def action_env(self) -> None:
        # Refresh once the editor screen is popped -- the admin password,
        # server address, etc. it just edited are all shown right here too.
        self.app.push_screen(EnvEditorScreen(), lambda _: self.refresh_config_widgets())

    def action_quit_panel(self) -> None:
        self.app.exit()

    def _run_interactive(self, cmd: list[str]) -> str:
        """Runs an interactive helper (the naming migration scripts) with the
        real terminal handed back to it via suspend(), capturing only its
        stdout (the scripts write prompts to stderr specifically so this
        works). Must be called from the main thread -- suspend() manipulates
        the app's terminal driver directly."""
        with self.app.suspend():
            proc = subprocess.run(cmd, cwd=ROOT, stdout=subprocess.PIPE, text=True)
        return proc.stdout or ""

    def _resolve_naming(self) -> tuple[str, str]:
        """Resolves Docker/DB naming, preferring the cached decision so
        repeated Start/Restart never touches app.suspend() once a machine
        has answered the prompt once. suspend()/resume is only exercised the
        very first time, when there's an actual decision to make."""
        cached = MANAGER.cached_docker_naming()
        if cached is not None:
            project, prefix = cached, cached
        else:
            self.notify("Resolving Docker naming...")
            project, prefix = MANAGER.resolve_docker_naming(self._run_interactive)
        if prefix == "owpengram" and MANAGER.cached_db_naming() is None:
            MANAGER.resolve_db_naming(self._run_interactive, f"{prefix}-postgres")
        return project, prefix

    def action_start(self) -> None:
        status = MANAGER.status()
        if status.running:
            self.notify("Already running", severity="warning")
            return
        if not ENV_FILE.exists():
            self.notify(".env not found — copy .env.example to .env and configure it first", severity="error")
            return
        self.query_one(OptionList).disabled = True
        self._begin_start()

    def _begin_start(self) -> None:
        """Runs on the main thread: naming resolution may need suspend()."""
        try:
            project, prefix = self._resolve_naming()
        except Exception as exc:  # noqa: BLE001 - never leave the menu stuck
            self.notify(f"Naming resolution failed: {exc}", severity="error", timeout=10)
            self._unlock_menu()
            return
        self._start_rest(project, prefix)

    @work(thread=True, exclusive=True)
    def _start_rest(self, project: str, prefix: str) -> None:
        try:
            self.app.call_from_thread(self.notify, "Starting Docker infrastructure...")
            ok, out = MANAGER.docker_compose_up(project, prefix)
            if not ok:
                self.app.call_from_thread(self.notify, f"docker compose up failed:\n{out[-500:]}", severity="error", timeout=10)
                return

            self.app.call_from_thread(self.notify, "Waiting for PostgreSQL...")
            if not MANAGER.wait_postgres(prefix):
                self.app.call_from_thread(self.notify, "PostgreSQL not ready after 60s", severity="error")
                return

            self.app.call_from_thread(self.notify, "Building binaries...")
            ok, out = MANAGER.build()
            if not ok:
                self.app.call_from_thread(self.notify, f"Build failed:\n{out[-500:]}", severity="error", timeout=10)
                return

            server_pid = MANAGER.launch(SERVER_EXE, SERVER_LOG)
            admin_pid = MANAGER.launch(ADMIN_EXE, ADMIN_LOG)
            save_state({
                "server_pid": server_pid,
                "admin_pid": admin_pid,
                "docker_project": project,
                "docker_prefix": prefix,
            })
            self.app.call_from_thread(self.notify, "Server started")
        except Exception as exc:  # noqa: BLE001 - never leave the menu stuck
            self.app.call_from_thread(self.notify, f"Start failed: {exc}", severity="error", timeout=10)
        finally:
            self.app.call_from_thread(self._unlock_menu)

    def _unlock_menu(self) -> None:
        option_list = self.query_one(OptionList)
        option_list.disabled = False
        # Disabling a widget blurs it, and Textual doesn't automatically
        # restore focus when it's re-enabled: the number-key/q bindings
        # (screen-level) kept working regardless, but arrow-key navigation,
        # Enter-to-select and mouse clicks on the list all silently stopped
        # doing anything until something explicitly refocused it.
        option_list.focus()
        self.refresh_status()

    def action_stop(self) -> None:
        status = MANAGER.status()
        if not status.running:
            self.notify("Not running", severity="warning")
            return
        self.query_one(OptionList).disabled = True
        self.notify("Stopping...")
        self._stop_worker()

    @work(thread=True, exclusive=True)
    def _stop_worker(self) -> None:
        try:
            MANAGER.stop()
            self.app.call_from_thread(self.notify, "Stopped")
        except Exception as exc:  # noqa: BLE001 - never leave the menu stuck
            self.app.call_from_thread(self.notify, f"Stop failed: {exc}", severity="error", timeout=10)
        finally:
            self.app.call_from_thread(self._unlock_menu)

    def action_restart(self) -> None:
        self.query_one(OptionList).disabled = True
        self._restart_worker()

    @work(thread=True, exclusive=True)
    def _restart_worker(self) -> None:
        try:
            status = MANAGER.status()
            if status.running:
                self.app.call_from_thread(self.notify, "Restarting: stopping...")
                MANAGER.stop()
        except Exception as exc:  # noqa: BLE001 - never leave the menu stuck
            self.app.call_from_thread(self.notify, f"Restart (stop phase) failed: {exc}", severity="error", timeout=10)
            self.app.call_from_thread(self._unlock_menu)
            return
        # Hand off to the main thread: starting back up needs to run there
        # (naming resolution may call suspend()), and _start_rest takes over
        # unlocking the menu once it's done.
        self.app.call_from_thread(self._begin_start)


class ServerPanelApp(App):
    TITLE = "OwpenGram Server Panel"
    CSS = """
    #banner {
        width: 100%;
        /* content-align centers the whole multi-line block as one unit;
        text-align would instead justify each line independently by its own
        width, which staggers hand-aligned ASCII art like this banner. */
        content-align: center middle;
        color: cyan;
        text-style: bold;
        margin: 1 1 0 1;
    }
    #main-body {
        height: 1fr;
        margin: 1 1 0 1;
    }
    .panel-title {
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }
    #services-panel {
        width: 2fr;
        height: 100%;
        border: round $accent;
        padding: 1 2;
        margin-right: 1;
    }
    #services-table {
        height: 1fr;
    }
    #sidebar {
        width: 1fr;
        height: 100%;
        border: round $accent;
        padding: 1 2;
        overflow-y: auto;
    }
    .stat-label {
        margin-top: 1;
    }
    #sidebar ProgressBar {
        width: 100%;
    }
    #admin-link {
        margin-top: 1;
    }
    #admin-password, #server-address, #server-public-key {
        margin-top: 1;
    }
    #env-title {
        text-style: bold;
        color: $accent;
        padding: 1 2 0 2;
    }
    #env-missing {
        padding: 1 2;
    }
    #env-scroll {
        height: 1fr;
        padding: 1 2;
    }
    .env-field {
        height: auto;
        margin: 1 0 0 2;
    }
    .env-field-key {
        color: $accent;
    }
    .env-field-desc {
        color: $text-muted;
        margin-bottom: 1;
    }
    #env-actions {
        height: auto;
        padding: 1 2;
        align: right middle;
    }
    #env-actions Button {
        margin-left: 1;
    }
    LogTailScreen Horizontal {
        height: 1fr;
    }
    LogTailScreen Vertical {
        width: 1fr;
        height: 1fr;
    }
    RichLog {
        border: round $accent;
        height: 1fr;
    }
    """

    def on_mount(self) -> None:
        self.push_screen(MainScreen())


if __name__ == "__main__":
    ServerPanelApp().run()
