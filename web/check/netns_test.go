package check

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const networkParentEnv = "AACP_CHECK_PARENT_NETNS"

var networkUnavailable string

// Host link changes reach Chrome through netlink and can cancel module loads
// in flight. The fixture servers and Chrome share a loopback-only network so
// another run's Docker bridges cannot interrupt this run. Filesystem Unix
// sockets, including a unix:path session D-Bus, remain reachable; abstract
// Unix sockets belong to the network namespace and do not cross this boundary.
func runInNetworkNamespace() (code int, reran bool) {
	if os.Getenv(networkParentEnv) != "" {
		status := os.NewFile(3, "network namespace startup")
		if err := bringLoopbackUp(); err != nil {
			fmt.Fprint(status, err)
			status.Close()
			return 1, true
		}
		fmt.Fprint(status, "ready")
		status.Close()
		return 0, false
	}
	code, err := restartInNetworkNamespace()
	if err == nil {
		return code, true
	}
	networkUnavailable = err.Error()
	fmt.Fprintf(os.Stderr, "web/check: network namespace unavailable; running on the host network: %s\n", networkUnavailable)
	return 0, false
}

func restartInNetworkNamespace() (int, error) {
	parent, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		return 0, err
	}
	binary, err := os.Executable()
	if err != nil {
		return 0, err
	}
	status, childStatus, err := os.Pipe()
	if err != nil {
		return 0, err
	}
	defer status.Close()
	defer childStatus.Close()
	cmd := exec.Command(binary, os.Args[1:]...)
	cmd.Env = append(os.Environ(), networkParentEnv+"="+parent)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.ExtraFiles = []*os.File{childStatus}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:  unix.CLONE_NEWUSER | unix.CLONE_NEWNET,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: os.Getuid(), HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: os.Getgid(), HostID: os.Getgid(), Size: 1}},
		// A nonzero uid keeps Chrome's sandbox enabled. CAP_NET_ADMIN must
		// survive exec to raise lo; it grants rights only in this new network.
		AmbientCaps: []uintptr{unix.CAP_NET_ADMIN},
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	childStatus.Close()
	ready, readErr := io.ReadAll(status)
	waitErr := cmd.Wait()
	if readErr != nil {
		return 0, readErr
	}
	// Only startup failures fall back. Repeating a failed test on the host
	// would hide failures and reintroduce the network race it was testing.
	if string(ready) != "ready" {
		return 0, fmt.Errorf("namespace startup did not finish: %s (child: %v)", ready, waitErr)
	}
	if waitErr != nil {
		if exited, ok := waitErr.(*exec.ExitError); ok {
			return exited.ExitCode(), nil
		}
		return 1, nil
	}
	return 0, nil
}

func bringLoopbackUp() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	request, err := unix.NewIfreq("lo")
	if err != nil {
		return err
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, request); err != nil {
		return err
	}
	request.SetUint16(request.Uint16() | unix.IFF_UP)
	return unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, request)
}

func TestFixturesHaveTheirOwnNetwork(t *testing.T) {
	if networkUnavailable != "" {
		t.Skipf("network namespace unavailable: %s", networkUnavailable)
	}
	parent := os.Getenv(networkParentEnv)
	if parent == "" {
		t.Fatal("the test binary did not attempt a restart in its own network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	if current == parent {
		t.Errorf("the fixture network %s is still the parent's network", current)
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(interfaces) != 1 || interfaces[0].Name != "lo" {
		t.Fatalf("the fixture network has more than loopback: %v", interfaces)
	}
	if interfaces[0].Flags&net.FlagUp == 0 {
		t.Error("loopback is down")
	}
	for _, mapping := range []struct {
		file string
		id   int
	}{{"uid_map", os.Getuid()}, {"gid_map", os.Getgid()}} {
		raw, err := os.ReadFile("/proc/self/" + mapping.file)
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("%d %d 1", mapping.id, mapping.id)
		if got := strings.Join(strings.Fields(string(raw)), " "); got != want {
			t.Errorf("%s = %q, want the current identity mapped to itself: %q", mapping.file, got, want)
		}
	}
}
