# Cellbox operations

Cellbox is intended to run directly on a Linux host or VM. The native backend needs delegated cgroup v2 controllers and the kernel namespace, overlayfs, and seccomp features checked by `cellbox doctor`. Run `cellbox doctor` as the same account and from the same service manager that will run `cellbox serve`.

## User-local release install

The release installer targets Linux amd64 and arm64, verifies the selected archive against the release SHA-256 manifest, and installs it without sudo:

```sh
curl -fsSL https://raw.githubusercontent.com/Senasphy/cellbox/main/install.sh | bash
curl -fsSL https://raw.githubusercontent.com/Senasphy/cellbox/main/install.sh | bash -s -- 1.2.3
```

For a private repository, check out the repository using your configured GitHub credentials and run `GH_TOKEN=... ./install.sh`; the token needs permission to read releases. To choose a different repository or install directory, set `CELLBOX_REPOSITORY` or `CELLBOX_INSTALL_DIR`. Review the script before piping it to a shell. The release workflow must publish `cellbox_<version>_linux_<arch>.tar.gz` and `cellbox_checksums.txt`, as configured in `.goreleaser.yaml`.

## System service

Install the binary at `/usr/bin/cellbox`, then create a dedicated unprivileged account and install the unit:

```sh
sudo install -Dm755 ./cellbox /usr/bin/cellbox
sudo useradd --system --create-home --home-dir /var/lib/cellbox --shell /usr/bin/nologin cellbox
sudo install -Dm644 deploy/cellbox.service /etc/systemd/system/cellbox.service
sudo systemctl daemon-reload
sudo systemd-run --scope --uid=cellbox --property=Delegate=yes /usr/bin/cellbox doctor
sudo systemctl enable --now cellbox.service
sudo journalctl -u cellbox.service -f
```

The unit sets `Delegate=yes` for the native runtime, restarts on failure, stores state in `/var/lib/cellbox`, and applies systemd filesystem and network-family restrictions. It listens on loopback by default. The first start prints an administrator API key once; capture it from the service journal and store it securely. Do not expose the service remotely without TLS and the explicit remote-bind option.

## User service for laptops

Install the binary at `~/.local/bin/cellbox` and the unit in the user manager:

```sh
install -Dm755 ./cellbox ~/.local/bin/cellbox
mkdir -p ~/.config/systemd/user
install -m644 deploy/cellbox-user.service ~/.config/systemd/user/cellbox.service
systemctl --user daemon-reload
systemd-run --user --scope --property=Delegate=yes ~/.local/bin/cellbox doctor
systemctl --user enable --now cellbox.service
systemctl --user status cellbox.service
```

The user unit stores state in `~/.local/share/cellbox` and also requests cgroup delegation. User-manager delegation varies by distribution and login configuration; if `cellbox doctor` reports missing controllers, configure the host's user slice before relying on native resource limits. To keep the service running after logout, enable lingering for that account with the system administrator's normal systemd procedure.

## Docker deployment

The recommended deployment is directly on a Linux host or VM. Running Cellbox inside Docker requires nested user namespaces and delegated cgroups, which default Docker seccomp and AppArmor profiles commonly block. A demo container needs at least a writable delegated cgroup subtree, nested user and mount namespaces, and permission for the required overlay and seccomp operations. Those settings weaken the container boundary; do not use the demo container as a hostile-workload isolation boundary. The Docker runtime backend is a separate compatibility backend and does not provide the native backend's security guarantees.

`Dockerfile` and `deploy/compose.demo.yaml` provide a local dashboard demo using the Docker backend:

```sh
docker compose -f deploy/compose.demo.yaml up --build
```

This demo mounts the host Docker socket into Cellbox. Anyone who can reach that socket through Cellbox can ask the host daemon to create privileged containers and access host resources; treat the socket as host-root access. The HTTP port is bound to `127.0.0.1`, but the demo does not configure TLS, and its default admin key is printed in the container logs. Do not expose this demo on an untrusted network or use it as a production security boundary. It runs the Docker backend, not the rootless native backend.
