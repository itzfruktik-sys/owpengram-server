//go:build windows

package hoststats

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// x/sys/windows doesn't wrap GlobalMemoryStatusEx (unlike GetDiskFreeSpaceEx,
// which diskFreeBytes uses directly), so it's called through kernel32.dll by
// hand -- the same LazyDLL/NewProc pattern the x/sys/windows package itself
// uses internally for the calls it does wrap.
var (
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
)

// memoryStatusEx mirrors the Win32 MEMORYSTATUSEX struct. Field order and
// sizes must match exactly -- this is passed by pointer straight to the
// syscall.
type memoryStatusEx struct {
	cbSize                  uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

// memStats reads host RAM usage via GlobalMemoryStatusEx.
func memStats() (used, total int64, err error) {
	var m memoryStatusEx
	m.cbSize = uint32(unsafe.Sizeof(m))
	r, _, callErr := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return 0, 0, fmt.Errorf("hoststats: GlobalMemoryStatusEx: %w", callErr)
	}
	total = int64(m.ullTotalPhys)
	used = total - int64(m.ullAvailPhys)
	if used < 0 {
		used = 0
	}
	return used, total, nil
}
