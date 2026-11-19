# NixOS VM test for the dmanager module: Docker and the service start with no
# network and no Tailscale, the UI and API answer, and a container carrying the
# `dmanager.managed-by` label is reported as externally managed.
{ self, pkgs }:
let
  # Tiny image built locally: the VM has no registry access.
  sleeperImage = pkgs.dockerTools.buildImage {
    name = "dmanager-vm-sleeper";
    tag = "latest";
    copyToRoot = [ pkgs.busybox ];
    config.Cmd = [ "sleep" "3600" ];
  };
in
pkgs.testers.runNixOSTest {
  name = "dmanager-nixos-module";

  nodes.machine = {
    imports = [ self.nixosModules.default ];

    virtualisation.memorySize = 2048;
    virtualisation.additionalPaths = [ sleeperImage ];
    virtualisation.docker.enable = true;
    environment.systemPackages = [ pkgs.curl pkgs.jq ];

    services.dmanager = {
      enable = true;
      openFirewall = true;
      # Plain HTTP in the VM: the session cookie must not require TLS.
      settings.auth.secure_cookies = "never";
    };
  };

  testScript = ''
    machine.wait_for_unit("docker.service")
    machine.wait_for_unit("dmanager.service")
    machine.wait_for_open_port(9283)

    with subtest("UI and API answer"):
        machine.succeed("curl -fsS http://127.0.0.1:9283/ | grep -i '<html'")
        status = machine.succeed(
            "curl -fsS -X POST -H 'Content-Type: application/json' -d '{}' "
            "http://127.0.0.1:9283/dmanager.v1.AuthService/GetServerStatus"
        )
        assert '"needsSetup":true' in status.replace(" ", ""), status

    with subtest("process identity and state directory"):
        pid = machine.succeed("systemctl show -P MainPID dmanager.service").strip()
        assert pid != "0", "dmanager.service has no main PID"
        user = machine.succeed(f"stat -c %U /proc/{pid}").strip()
        assert user == "dmanager", f"service runs as {user!r}"
        groups = machine.succeed("id -nG dmanager").split()
        assert "docker" not in groups, f"user is in the docker group: {groups}"
        perms = machine.succeed("stat -c '%a %U:%G' /var/lib/dmanager").strip()
        assert perms == "700 dmanager:dmanager", f"state dir is {perms!r}"
        machine.succeed("test -f /var/lib/dmanager/dmanager.db")

    with subtest("labelled container is reported as externally managed"):
        machine.succeed("docker load < ${sleeperImage}")
        machine.succeed(
            "docker run -d --name vm-managed --label dmanager.managed-by=nixos "
            "dmanager-vm-sleeper:latest"
        )
        machine.succeed(
            "docker run -d --name vm-plain dmanager-vm-sleeper:latest"
        )
        # The API is behind the first-run setup, so create the admin through
        # the API and log in with a cookie jar.
        api = "http://127.0.0.1:9283/dmanager.v1"
        post = "curl -fsS -X POST -H 'Content-Type: application/json' -b /tmp/jar -c /tmp/jar"
        creds = '{"username":"admin","password":"vm-test-passphrase-123"}'
        machine.succeed(f"{post} -d '{creds}' {api}.AuthService/SetupAdmin")
        machine.succeed(f"{post} -d '{creds}' {api}.AuthService/Login")
        machine.wait_until_succeeds(
            f"{post} -d '{{}}' {api}.ContainerService/ListContainers"
            " | jq -e '[.containers[] | select(.name | ltrimstr(\"/\") == \"vm-managed\")"
            " | select(.externallyManaged == true and .managedBy == \"nixos\")] | length == 1'",
            timeout=60,
        )
        machine.succeed(
            f"{post} -d '{{}}' {api}.ContainerService/ListContainers"
            " | jq -e '[.containers[] | select(.name | ltrimstr(\"/\") == \"vm-plain\")"
            " | select(.externallyManaged != true)] | length == 1'"
        )

    with subtest("clean stop"):
        machine.succeed("systemctl stop dmanager.service")
        result = machine.succeed("systemctl show -P Result dmanager.service").strip()
        assert result == "success", f"stopped with result {result!r}"
  '';
}
