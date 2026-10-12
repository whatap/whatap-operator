# KubeVirt first-boot cloud-init renderer

`Render(existingUserData []byte, Config) ([]byte, error)` is a pure renderer.
Its output **contains the license** and must be stored only in a newly generated
Kubernetes Secret referenced through `cloudInitNoCloud.secretRef`. Never place
it in VM inline `userData`, annotations, logs, errors, or command arguments.
Secret ownership, immutable Secret lifecycle, existing base64/Secret decoding,
VM admission/start gating, and separate `networkData` preservation are callers'
responsibility. The output exceeds KubeVirt's 2,048-byte inline user-data limit.

## Input contract

- Empty input or one `#cloud-config` YAML mapping; existing mapping order and
  unrelated values are retained, with `write_files` and `runcmd` appended.
- Reject shell/MIME input, multiple YAML documents, duplicate keys, malformed
  typed scalars, aliases/anchors, custom tags, YAML/cloud-init merge directives,
  module-list overrides, invalid bootstrap lists, and reserved path collisions.
- Input is limited to 1 MiB and nesting to 64 levels. This is not a complete
  cloud-init schema validator: existing commands are trusted user configuration.
- License: nonblank printable ASCII, at most 8,192 bytes; no control characters.
  Connection values are preserved literally in the config file via YAML node
  encoding, never interpolated into executable shell. Host: slash-separated IP/DNS names.
  Port: 1–65535, or zero for the default 6600.
- AgentVersion: an official numeric X.Y.Z tar release (at most 32 characters).
  Omission selects 2.9.25. Changing the policy only affects future VM CREATEs;
  it does not update already-rendered Secrets or installed guests.

## Guest bootstrap

The image must already have active cloud-init, Bash, systemd, curl, tar and
trusted CA certificates. Supported
`/etc/os-release` IDs are `debian`, `ubuntu`, `rhel`, `centos`, `rocky`,
`almalinux`, `ol`, and `amzn`. Fedora and other systems fail closed. The URL's
architecture is amd64 for x86_64, or arm64 for aarch64/arm64. Availability and
native library compatibility still require testing each OS/version/architecture.

Cloud-init writes a root-owned 0700 script and 0600 connection file. The script
downloads the requested tar from the fixed official HTTPS repo.whatap.io/infra
path on every invocation and calls its install.sh, without rpm/dpkg queries or
package/repository changes. It prepares the private configuration before the
installer, then explicitly enables, starts and checks the service. The official
tar installer registers the service but does not start it. The installer runs
under the standard umask 022 because it writes systemd units without chmod;
the script itself keeps umask 077 and sets 0600 on secret files explicitly.
HTTPS and archive/version checks are not a claim of a publisher signature or
package GPG checks.
No license is passed in a process argument or sourced as shell code.

A root-only ownership snapshot protects existing credentials, not an install
skip flag: a manual rerun downloads and invokes the tar installer again.
A pre-existing foreign agent, symlink, changed connection input, or externally
modified agent config is refused rather than silently taken over. This covers
pre-baked agent images or reused disks; normal new cloud-init images have no
agent to take over. A new Kubernetes VM object alone does not prove fresh disk
state. Inconsistent partial state fails closed and can require inspection.
The `success` file records an install/service-start success, not current health
or telemetry delivery. It is not a control-plane status channel.

## Verification boundary

Run only the isolated package, never this operator's cluster-changing E2E suite:

```sh
KUBECONFIG=/dev/null go test -p=1 -count=1 ./internal/kubevirt/cloudinit
KUBECONFIG=/dev/null go vet ./internal/kubevirt/cloudinit
```

The tests parse actual renderer output, run `bash -n` on the unmodified emitted
script, and execute that script with guest paths redirected into a private
scratch directory. Network fetches, the installer, identity, ownership and
systemctl are test doubles; archive handling, file comparisons, permissions, branching
and rerun behavior execute for real. This is a **redirected-filesystem harness**,
not real Linux/cloud-init/KubeVirt E2E. The official installer, systemd
behavior on each image, release availability, native agent configuration parsing,
and collector connectivity still need new-VM acceptance testing. No tests
install packages on the host, access credentials, or contact a cluster/network.
