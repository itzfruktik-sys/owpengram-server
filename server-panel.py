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
import signal
import subprocess
import time
from dataclasses import dataclass
from pathlib import Path

from textual import work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical
from textual.screen import Screen
from textual.widgets import Footer, Header, Label, OptionList, RichLog, Static
from textual.widgets.option_list import Option

IS_WINDOWS = platform.system() == "Windows"

ROOT = Path(__file__).resolve().parent
DEPLOY_DIR = ROOT / "deploy"
BIN_DIR = ROOT / "bin"
LOG_DIR = ROOT / "logs"
ENV_FILE = ROOT / ".env"
COMPOSE_FILE = DEPLOY_DIR / "docker-compose.yml"
STATE_FILE = ROOT / ".server_panel.json"

SERVER_EXE = BIN_DIR / ("owpengram-server.exe" if IS_WINDOWS else "owpengram-server")
ADMIN_EXE = BIN_DIR / ("owpengram-admin-panel.exe" if IS_WINDOWS else "owpengram-admin-panel")
SERVER_LOG = LOG_DIR / "owpengram-server.log"
ADMIN_LOG = LOG_DIR / "owpengram-admin-panel.log"


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

    @property
    def running(self) -> bool:
        return self.server_alive or self.admin_alive


class ServerManager:
    """Owns the actual start/stop/build/launch mechanics. No Textual
    dependency in here on purpose, so it stays easy to reason about /
    reuse outside the TUI if that's ever useful."""

    def status(self) -> Status:
        state = load_state()
        server_pid = state.get("server_pid")
        admin_pid = state.get("admin_pid")
        return Status(
            server_pid=server_pid,
            server_alive=pid_alive(server_pid),
            admin_pid=admin_pid,
            admin_alive=pid_alive(admin_pid),
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


# --- UI -----------------------------------------------------------------


def status_text(status: Status) -> str:
    def line(name: str, pid: int | None, alive: bool) -> str:
        if alive:
            return f"  {name}: [b green]RUNNING[/] (PID {pid})"
        if pid:
            return f"  {name}: [b red]STOPPED[/] (last PID {pid} not alive)"
        return f"  {name}: [dim]not started[/]"

    return (
        line("owpengram-server", status.server_pid, status.server_alive)
        + "\n"
        + line("owpengram-admin-panel", status.admin_pid, status.admin_alive)
    )


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


class MainScreen(Screen):
    BINDINGS = [
        Binding("1", "start", "Start"),
        Binding("2", "stop", "Stop"),
        Binding("3", "restart", "Restart"),
        Binding("4", "logs", "Logs"),
        Binding("q", "quit_panel", "Exit"),
    ]

    def compose(self) -> ComposeResult:
        yield Header()
        yield Static(id="status")
        yield OptionList(
            Option("Start server", id="start"),
            Option("Stop server", id="stop"),
            Option("Restart server", id="restart"),
            Option("View logs", id="logs"),
            Option("Exit panel (server keeps running)", id="exit"),
        )
        yield Footer()

    def on_mount(self) -> None:
        self.refresh_status()
        self.set_interval(3, self.refresh_status)

    def refresh_status(self) -> None:
        self.query_one("#status", Static).update(status_text(MANAGER.status()))

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
        elif option_id == "exit":
            self.action_quit_panel()

    def action_logs(self) -> None:
        self.app.push_screen(LogPickerScreen())

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
    #status {
        padding: 1 2;
        border: round $accent;
        margin: 1 1 0 1;
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
