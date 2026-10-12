package cloudinit

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This is a redirected-filesystem harness, NOT a Linux guest/cloud-init E2E.
// Every guest path is redirected into t.TempDir, identity/systemd/network/package
// commands are fake, and the real emitted Bash is syntax checked unmodified.
// Archives are real gzip/tar bytes, extracted by the host tar, not fake listings.
func newBootstrapHarness(t *testing.T, distro string, version ...string) (string, string) {
	t.Helper()
	config := Config{License: testLicense, Host: "collector.example"}
	archiveVersion := DefaultAgentVersion
	if len(version) > 0 {
		config.AgentVersion = version[0]
		archiveVersion = version[0]
	}
	data, err := Render(nil, config)
	if err != nil {
		t.Fatal(err)
	}
	files := decodeConfig(t, data)["write_files"].([]interface{})
	script := files[0].(map[string]interface{})["content"].(string)
	check := exec.Command("/bin/bash", "-n")
	check.Stdin = strings.NewReader(script)
	if err := check.Run(); err != nil {
		t.Fatal("emitted script fails bash -n")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"etc/whatap-kubevirt", "run/systemd/system", "bin", "state"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeHarnessFile(t, root, "etc/os-release", "ID="+distro+"\n", 0600)
	writeHarnessFile(t, root, "state/arch", "x86_64\n", 0600)
	writeBootstrapArchive(t, root, archiveVersion)
	writeHarnessFile(t, root, "etc/whatap-kubevirt/whatap.conf", files[1].(map[string]interface{})["content"].(string), 0600)
	// No production path-injection mechanism: only the test transforms fixed paths.
	script = strings.NewReplacer("/etc/", root+"/etc/", "/usr/whatap/", root+"/usr/whatap/", "/var/lib/", root+"/var/lib/", "/run/systemd/system", root+"/run/systemd/system", "PATH=/usr/sbin:/usr/bin:/sbin:/bin", "PATH="+root+"/bin:/usr/bin:/bin").Replace(script)
	writeHarnessFile(t, root, "bootstrap.sh", script, 0700)
	fake := `#!/bin/bash
set -eu
name=${0##*/}
printf '%s ' "$name" "$@" >> "$HARNESS_ROOT/commands"
printf '\n' >> "$HARNESS_ROOT/commands"
case "$name" in
 id) printf '0\n';;
 uname)
   if test "$1" = -m; then /bin/cat "$HARNESS_ROOT/state/arch"; else printf 'Linux\n'; fi;;
 chown) :;;
 apt-get|dnf|yum|dpkg-query|rpm|gpg) exit 95;;
 tar) exec /usr/bin/tar "$@";;
 systemctl)
   case "$1" in
    is-enabled)
      if test -f "$HARNESS_ROOT/state/masked"; then printf 'masked\n'; exit 1; fi
      test -f "$HARNESS_ROOT/state/enabled" && printf 'enabled\n';;
    enable)
      test ! -f "$HARNESS_ROOT/state/fail-enable"
      test ! -f "$HARNESS_ROOT/state/masked"
      cmp -s "$HARNESS_ROOT/usr/whatap/infra/conf/whatap.conf" "$HARNESS_ROOT/var/lib/whatap-kubevirt/managed.conf"
      : > "$HARNESS_ROOT/state/enabled";;
    start)
      test ! -f "$HARNESS_ROOT/state/fail-start"
      test ! -f "$HARNESS_ROOT/state/masked"
      cmp -s "$HARNESS_ROOT/usr/whatap/infra/conf/whatap.conf" "$HARNESS_ROOT/var/lib/whatap-kubevirt/managed.conf"
      : > "$HARNESS_ROOT/state/active";;
    is-active) test ! -f "$HARNESS_ROOT/state/fail-active" && test -f "$HARNESS_ROOT/state/active";;
    daemon-reload) test ! -f "$HARNESS_ROOT/state/fail-reload";;
    *) exit 96;;
   esac;;
 curl)
   test ! -f "$HARNESS_ROOT/state/fail-download"
   output=''
   while test "$#" -gt 0; do
     case "$1" in -o|--output) shift; output=$1;; esac
     shift
   done
   test -n "$output"
   cp "$HARNESS_ROOT/release.tar.gz" "$output";;
 *) exit 97;;
esac
`
	for _, name := range []string{"id", "uname", "chown", "dpkg-query", "rpm", "systemctl", "apt-get", "dnf", "yum", "curl", "gpg", "tar"} {
		writeHarnessFile(t, root, "bin/"+name, fake, 0700)
	}
	return root, filepath.Join(root, "bootstrap.sh")
}

func writeHarnessFile(t *testing.T, root, name, content string, mode os.FileMode) {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func runBootstrap(root, script string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", script)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + root, "HARNESS_ROOT=" + root, "TMPDIR=" + root}
	return cmd.CombinedOutput()
}

// The fixture follows the observed official 2.9.25 archive layout and installer
// contract: --prefix only, prewritten config preserved, register but never start.
// It is NOT the real Linux binary/installer and never installs host services.
func writeBootstrapArchive(t *testing.T, root, version string, extra ...tar.Header) {
	t.Helper()
	installer := `#!/bin/bash
set -eu
printf 'installer %s %s\n' "$@" >> "$HARNESS_ROOT/commands"
test "$#" = 2 && test "$1" = --prefix
prefix=$2
test "$prefix" = "$HARNESS_ROOT/usr/whatap/infra"
test -s "$prefix/conf/whatap.conf"
cmp -s "$prefix/conf/whatap.conf" "$HARNESS_ROOT/var/lib/whatap-kubevirt/managed.conf"
mode=$(/usr/bin/stat -c '%a' "$prefix/conf/whatap.conf" 2>/dev/null) || mode=$(/usr/bin/stat -f '%Lp' "$prefix/conf/whatap.conf")
test "$mode" = 600
test ! -f "$HARNESS_ROOT/state/fail-installer"
cp "$(dirname "$0")/conf/VERSION" "$prefix/conf/VERSION"
if test -f "$HARNESS_ROOT/state/wrong-installed-version"; then printf '0.0.0\n' > "$prefix/conf/VERSION"; fi
if test -f "$HARNESS_ROOT/state/overwrite-config"; then printf 'installer-default\n' > "$prefix/conf/whatap.conf"; fi
# Like the official 2.9.25 installer: systemd units are written by cat > with no chmod.
printf '[Unit]\n' > "$HARNESS_ROOT/state/installer-unit.service"
systemctl daemon-reload
systemctl enable whatap-infra.service || true
`
	file, err := os.Create(filepath.Join(root, "release.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for _, entry := range []struct{ name, body string }{
		{"whatap-infra/", ""}, {"whatap-infra/conf/", ""},
		{"whatap-infra/conf/VERSION", version + "\n"},
		{"whatap-infra/whatap_infrad", "fixture-not-an-agent\n"},
		{"whatap-infra/infra_agent_patch.sh", "#!/bin/bash\nexit 99\n"},
		{"whatap-infra/install.sh", installer},
		{"whatap-infra/uninstall.sh", "#!/bin/bash\nexit 99\n"},
	} {
		header := tar.Header{Name: entry.name, Mode: 0700, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(entry.name, "/") {
			header.Typeflag = tar.TypeDir
		}
		if err := tw.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, header := range extra {
		if err := tw.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapDownloadsOfficialTarEveryInvocation(t *testing.T) {
	root, script := newBootstrapHarness(t, "debian")
	for i := 0; i < 2; i++ {
		if _, err := runBootstrap(root, script); err != nil {
			commands, _ := os.ReadFile(filepath.Join(root, "commands"))
			t.Fatalf("bootstrap failed: %v; commands: %s", err, strings.ReplaceAll(string(commands), testLicense, "[REDACTED]"))
		}
	}
	commands, err := os.ReadFile(filepath.Join(root, "commands"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(commands), "https://repo.whatap.io/infra/whatap-infra-2.9.25-linux-amd64.tar.gz") != 2 {
		t.Fatal("each invocation must download the official versioned tar archive")
	}
	if strings.Count(string(commands), "installer --prefix ") != 2 {
		t.Fatalf("each invocation must execute the tar installer, including reruns; commands: %s", strings.ReplaceAll(string(commands), testLicense, "[REDACTED]"))
	}
	if _, err := os.Stat(filepath.Join(root, "var/lib/whatap-kubevirt/success")); err != nil {
		t.Fatal("installer rerun did not finish service verification")
	}
}

func TestBootstrapPreservesPrivateConfigWithoutSecretArguments(t *testing.T) {
	root, script := newBootstrapHarness(t, "debian")
	for i := 0; i < 2; i++ {
		output, err := runBootstrap(root, script)
		if err != nil {
			t.Fatalf("bootstrap harness failed: %v (output suppressed)", err)
		}
		if strings.Contains(string(output), testLicense) {
			t.Fatal("secret leaked to output")
		}
	}
	config, err := os.ReadFile(filepath.Join(root, "usr/whatap/infra/conf/whatap.conf"))
	if err != nil || !strings.Contains(string(config), "license="+testLicense+"\n") || !strings.Contains(string(config), "createdtime=") {
		t.Fatal("final configuration missing")
	}
	info, err := os.Stat(filepath.Join(root, "usr/whatap/infra/conf/whatap.conf"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("final configuration not root-only")
	}
	if _, err := os.Stat(filepath.Join(root, "var/lib/whatap-kubevirt/success")); err != nil {
		t.Fatal("missing installation/service success marker")
	}
	commands, err := os.ReadFile(filepath.Join(root, "commands"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(commands), testLicense) || strings.Contains(string(commands), "--license") {
		t.Fatal("secret in argv")
	}
	for _, forbidden := range []string{"apt-get ", "dnf ", "yum ", "dpkg-query ", "rpm ", "gpg ", "systemctl mask", "systemctl unmask"} {
		if strings.Contains(string(commands), forbidden) {
			t.Fatalf("tar bootstrap invoked forbidden package/mask command %q", forbidden)
		}
	}
	for _, required := range []string{"curl -q ", "--proto =https ", "--proto-redir =https ", "--tlsv1.2 ", "--connect-timeout 10 ", "--max-time 120 ", "--retry 2 ", "--retry-max-time 300 ", "chown root:root " + filepath.Join(root, "usr/whatap/infra/conf/whatap.conf")} {
		if !strings.Contains(string(commands), required) {
			t.Fatalf("missing secure download/config contract %q", required)
		}
	}
	assertBootstrapCleaned(t, root)
}

// The official installer writes systemd units without chmod. Inheriting the
// bootstrap's private umask 077 makes them 0600, and systemd then warns
// "world-inaccessible" on every reload. Secret files keep explicit 0600 modes
// and files the bootstrap itself creates afterwards stay private.
func TestBootstrapRunsVendorInstallerWithStandardUmask(t *testing.T) {
	root, script := newBootstrapHarness(t, "ubuntu")
	if _, err := runBootstrap(root, script); err != nil {
		t.Fatalf("bootstrap harness failed: %v (output suppressed)", err)
	}
	for _, file := range []struct {
		name string
		want os.FileMode
	}{
		{"state/installer-unit.service", 0644},
		{"usr/whatap/infra/conf/whatap.conf", 0600},
		{"etc/whatap-kubevirt/whatap.conf", 0600},
		{"var/lib/whatap-kubevirt/input.conf", 0600},
		{"var/lib/whatap-kubevirt/managed.conf", 0600},
		{"var/lib/whatap-kubevirt/success", 0600},
	} {
		info, err := os.Stat(filepath.Join(root, file.name))
		if err != nil {
			t.Fatalf("%s missing: %v", file.name, err)
		}
		if got := info.Mode().Perm(); got != file.want {
			t.Fatalf("%s mode = %04o, want %04o", file.name, got, file.want)
		}
	}
}

func TestBootstrapEnablingIsNotStartingService(t *testing.T) {
	root, script := newBootstrapHarness(t, "ubuntu")
	// The tar installer registers/enables, but does not start the native service.
	if _, err := runBootstrap(root, script); err != nil {
		t.Fatal("tar installer completed but its service was not started")
	}
	if _, err := os.Stat(filepath.Join(root, "state/active")); err != nil {
		t.Fatal("native service is not active")
	}
	if _, err := os.Stat(filepath.Join(root, "var/lib/whatap-kubevirt/success")); err != nil {
		t.Fatal("service was not verified before recording installation success")
	}
	commands, _ := os.ReadFile(filepath.Join(root, "commands"))
	start := strings.Index(string(commands), "systemctl start whatap-infra.service")
	active := strings.Index(string(commands), "systemctl is-active --quiet whatap-infra.service")
	if start < 0 || active <= start || strings.Count(string(commands), "systemctl daemon-reload") != 2 || strings.Count(string(commands), "systemctl enable whatap-infra.service") != 2 {
		t.Fatal("bootstrap must explicitly reload, enable, start and check active after installer registration")
	}
}

func TestBootstrapIgnoresPackageDBAndLeavesRepositoriesUntouched(t *testing.T) {
	root, script := newBootstrapHarness(t, "debian")
	writeHarnessFile(t, root, "state/package-installed", "", 0600)
	writeHarnessFile(t, root, "etc/yum.repos.d/whatap-kubevirt.repo", "foreign-repo\n", 0600)
	for _, name := range []string{"etc/apt/trusted.gpg.d", "etc/apt/sources.list.d"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0711); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runBootstrap(root, script); err != nil {
		t.Fatal("bootstrap failed")
	}
	for _, name := range []string{"etc/apt/trusted.gpg.d", "etc/apt/sources.list.d"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || info.Mode().Perm() != 0711 {
			t.Fatal("changed permissions on a shared guest directory")
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "etc/yum.repos.d/whatap-kubevirt.repo")); err != nil || string(data) != "foreign-repo\n" {
		t.Fatal("existing repository overwritten")
	}
	for _, name := range []string{"etc/apt/trusted.gpg.d/whatap-release.gpg", "etc/apt/sources.list.d/whatap-kubevirt.list"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("unexpected repository file created")
		}
	}
}

func TestBootstrapSupportedOSArchitectureAndSelectedVersion(t *testing.T) {
	for _, distro := range []string{"ubuntu", "debian", "rhel", "centos", "rocky", "almalinux", "ol", "amzn"} {
		for machine, arch := range map[string]string{"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"} {
			t.Run(distro+"/"+machine, func(t *testing.T) {
				root, script := newBootstrapHarness(t, distro, "2.9.26")
				writeHarnessFile(t, root, "state/arch", machine+"\n", 0600)
				if _, err := runBootstrap(root, script); err != nil {
					t.Fatalf("supported guest failed: %v", err)
				}
				commands, _ := os.ReadFile(filepath.Join(root, "commands"))
				if !strings.Contains(string(commands), "https://repo.whatap.io/infra/whatap-infra-2.9.26-linux-"+arch+".tar.gz") {
					t.Fatal("wrong selected-version/architecture URL")
				}
				if _, err := os.Stat(filepath.Join(root, "var/lib/whatap-kubevirt/success")); err != nil {
					t.Fatal("installation not completed")
				}
			})
		}
	}
}

func TestBootstrapRefusesForeignStateAndNeverClaimsFailedService(t *testing.T) {
	for _, scenario := range []string{"foreign-config", "symlink", "unsupported", "fedora", "unsupported-arch", "failed-service", "changed-owned-config"} {
		t.Run(scenario, func(t *testing.T) {
			root, script := newBootstrapHarness(t, "debian")
			switch scenario {
			case "foreign-config":
				writeHarnessFile(t, root, "usr/whatap/infra/conf/whatap.conf", "foreign-config\n", 0600)
			case "symlink":
				writeHarnessFile(t, root, "foreign", "unchanged\n", 0600)
				if err := os.Symlink(filepath.Join(root, "foreign"), filepath.Join(root, "var")); err != nil {
					t.Fatal(err)
				}
			case "unsupported":
				writeHarnessFile(t, root, "etc/os-release", "ID=alpine\n", 0600)
			case "fedora":
				writeHarnessFile(t, root, "etc/os-release", "ID=fedora\n", 0600)
			case "unsupported-arch":
				writeHarnessFile(t, root, "state/arch", "s390x\n", 0600)
			case "failed-service":
				writeHarnessFile(t, root, "state/fail-start", "", 0600)
			case "changed-owned-config":
				if _, err := runBootstrap(root, script); err != nil {
					t.Fatal("initial install failed")
				}
				writeHarnessFile(t, root, "usr/whatap/infra/conf/whatap.conf", "foreign-config\n", 0600)
			}
			output, err := runBootstrap(root, script)
			if err == nil || strings.Contains(string(output), testLicense) {
				t.Fatal("conflict or service failure was not safely rejected")
			}
			if _, err := os.Stat(filepath.Join(root, "var/lib/whatap-kubevirt/success")); err == nil {
				t.Fatal("failed bootstrap claimed success")
			}
			if scenario == "foreign-config" || scenario == "changed-owned-config" {
				data, err := os.ReadFile(filepath.Join(root, "usr/whatap/infra/conf/whatap.conf"))
				if err != nil || string(data) != "foreign-config\n" {
					t.Fatal("foreign configuration overwritten")
				}
			}
		})
	}
}

func assertBootstrapCleaned(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "var/lib/whatap-kubevirt/download.*"))
	if err != nil || len(matches) != 0 {
		t.Fatal("temporary archive/extraction directory was not cleaned")
	}
}

func TestBootstrapFailureInvalidatesPreviousSuccess(t *testing.T) {
	for _, scenario := range []string{"fail-download", "fail-installer", "fail-enable", "fail-start", "fail-active", "fail-reload", "masked", "wrong-installed-version", "overwrite-config", "archive-version", "changed-input", "fedora", "arch"} {
		t.Run(scenario, func(t *testing.T) {
			root, script := newBootstrapHarness(t, "ubuntu")
			if _, err := runBootstrap(root, script); err != nil {
				t.Fatal("initial install failed")
			}
			if _, err := os.Stat(filepath.Join(root, "var/lib/whatap-kubevirt/success")); err != nil {
				t.Fatal("initial success was not recorded")
			}
			writeHarnessFile(t, root, "commands", "", 0600)
			switch scenario {
			case "archive-version":
				writeBootstrapArchive(t, root, "2.9.24")
			case "changed-input":
				writeHarnessFile(t, root, "etc/whatap-kubevirt/whatap.conf", "changed\n", 0600)
			case "fedora":
				writeHarnessFile(t, root, "etc/os-release", "ID=fedora\n", 0600)
			case "arch":
				writeHarnessFile(t, root, "state/arch", "ppc64le\n", 0600)
			default:
				writeHarnessFile(t, root, "state/"+scenario, "", 0600)
			}
			output, err := runBootstrap(root, script)
			if err == nil || strings.Contains(string(output), testLicense) {
				t.Fatal("failure was not propagated without leaking the license")
			}
			if _, err := os.Stat(filepath.Join(root, "var/lib/whatap-kubevirt/success")); !os.IsNotExist(err) {
				t.Fatal("failed rerun retained the previous success marker")
			}
			commands, _ := os.ReadFile(filepath.Join(root, "commands"))
			if strings.Contains(string(commands), testLicense) || strings.Contains(string(commands), "--license") {
				t.Fatal("failed rerun leaked license through argv")
			}
			if scenario != "fail-start" && scenario != "fail-active" && strings.Contains(string(commands), "systemctl start") {
				t.Fatal("service started after download/installer/configuration/registration failure")
			}
			if scenario == "archive-version" && strings.Contains(string(commands), "installer --prefix") {
				t.Fatal("installer executed despite archive VERSION mismatch")
			}
			assertBootstrapCleaned(t, root)
		})
	}
}

func TestBootstrapRejectsUnsafeArchivesBeforeExtraction(t *testing.T) {
	for _, header := range []tar.Header{
		{Name: "../escape", Typeflag: tar.TypeReg},
		{Name: "/absolute", Typeflag: tar.TypeReg},
		{Name: "whatap-infra/../../escape", Typeflag: tar.TypeReg},
		{Name: "whatap-infra/./ambiguous", Typeflag: tar.TypeReg},
		{Name: "whatap-infra/link", Typeflag: tar.TypeSymlink, Linkname: "../../escape"},
		{Name: "whatap-infra/hardlink", Typeflag: tar.TypeLink, Linkname: "whatap-infra/install.sh"},
		{Name: "whatap-infra/fifo", Typeflag: tar.TypeFifo},
		{Name: "whatap-infra/char", Typeflag: tar.TypeChar},
		{Name: "whatap-infra/block", Typeflag: tar.TypeBlock},
		{Name: "whatap-infra/space name", Typeflag: tar.TypeReg},
		{Name: "whatap-infra/new\nline", Typeflag: tar.TypeReg},
		{Name: "whatap-infra/conf/VERSION", Typeflag: tar.TypeReg},
		{Name: "whatap-infra/", Typeflag: tar.TypeDir},
		{Name: "other-root/file", Typeflag: tar.TypeReg},
		{Name: "whatap-infra/" + strings.Repeat("x", 110) + "/../../escape", Typeflag: tar.TypeReg, Format: tar.FormatPAX},
	} {
		t.Run(header.Name, func(t *testing.T) {
			root, script := newBootstrapHarness(t, "debian")
			writeBootstrapArchive(t, root, DefaultAgentVersion, header)
			output, err := runBootstrap(root, script)
			if err == nil || strings.Contains(string(output), testLicense) {
				t.Fatal("unsafe archive did not fail safely")
			}
			commands, _ := os.ReadFile(filepath.Join(root, "commands"))
			if strings.Contains(string(commands), "tar -x") || strings.Contains(string(commands), "installer --prefix") || strings.Contains(string(commands), "systemctl start") {
				t.Fatal("unsafe archive was extracted or executed")
			}
			if _, err := os.Stat(filepath.Join(root, "var/lib/whatap-kubevirt/success")); !os.IsNotExist(err) {
				t.Fatal("unsafe archive created a success marker")
			}
			assertBootstrapCleaned(t, root)
		})
	}
}

func TestBootstrapRejectsSymlinkedInstallerDestinations(t *testing.T) {
	for _, destination := range []string{
		"usr/whatap/infra/conf/whatap.conf", "usr/whatap/infra/conf/VERSION", "usr/whatap/infra/whatap_infrad",
		"usr/whatap/infra/infra_agent_patch.sh", "usr/whatap/infra/ChangeLog.txt", "etc/whatap/infra.meta",
		"etc/systemd/system/whatap-infra.service", "etc/systemd/system/whatap-updater.service",
		"etc/init.d/whatap-infra", "etc/init.d/whatap-updater",
	} {
		t.Run(destination, func(t *testing.T) {
			root, script := newBootstrapHarness(t, "debian")
			if _, err := runBootstrap(root, script); err != nil {
				t.Fatal("initial install failed")
			}
			writeHarnessFile(t, root, "commands", "", 0600)
			writeHarnessFile(t, root, "foreign", "unchanged\n", 0600)
			name := filepath.Join(root, destination)
			if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "foreign"), name); err != nil {
				t.Fatal(err)
			}
			if _, err := runBootstrap(root, script); err == nil {
				t.Fatal("symlinked installer destination accepted")
			}
			if data, err := os.ReadFile(filepath.Join(root, "foreign")); err != nil || string(data) != "unchanged\n" {
				t.Fatal("symlink target changed")
			}
			commands, _ := os.ReadFile(filepath.Join(root, "commands"))
			if strings.Contains(string(commands), "curl ") || strings.Contains(string(commands), "installer --prefix") {
				t.Fatal("symlink guard ran after download/installation")
			}
			if _, err := os.Stat(filepath.Join(root, "var/lib/whatap-kubevirt/success")); !os.IsNotExist(err) {
				t.Fatal("symlink conflict retained success marker")
			}
		})
	}
}
