# NixOS module: dmanager as a plain systemd service.
#
# A function of the flake's `self` so `package` defaults to the flake's own
# build: `nixosModules.default = import ./nix/module.nix self;`.
self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.dmanager;
  settingsFormat = pkgs.formats.yaml { };

  # Rendered into the world-readable store: only `*_file` paths may appear
  # here, never secret values (enforced by the assertion below).
  configFile = settingsFormat.generate "dmanager.yaml" cfg.settings;

  stateDir = "/var/lib/dmanager";

  absolutePath = lib.types.pathWith {
    inStore = false;
    absolute = true;
  };
  secretFileOption = description: example: lib.mkOption {
    type = lib.types.nullOr absolutePath;
    default = null;
    inherit example;
    description = ''
      ${description} Read by the service at start-up, so the file may live
      outside the state directory (for example under `/run`) and must be
      readable by {option}`services.dmanager.user`. Store paths are rejected:
      a path literal would be copied into the world-readable Nix store.
    '';
  };

  # Keys that must never carry a secret value in the rendered YAML.
  leakedKeys =
    lib.optional (cfg.settings ? tailscale && cfg.settings.tailscale ? auth_key) "tailscale.auth_key"
    ++ lib.optional (cfg.settings ? smtp && cfg.settings.smtp ? password) "smtp.password"
    ++ lib.optional (
      cfg.settings ? registries && lib.any (r: r ? password) cfg.settings.registries
    ) "registries[].password";

  optionalSet = cond: value: lib.optionalAttrs cond value;
in
{
  options.services.dmanager = {
    enable = lib.mkEnableOption ''
      dmanager, the Docker container update manager.

      The service talks to the Docker socket through the `docker` group,
      which is root-equivalent on the host: anything that can drive dmanager
      can start a privileged container. The unit's hardening limits what a
      compromised process can do directly, not what it can do through Docker
    '';

    package = lib.mkPackageOption self.packages.${pkgs.stdenv.hostPlatform.system} "default" {
      pkgsText = "dmanager.packages.\${system}";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "dmanager";
      description = ''
        System user the service runs as. It is created by this module when it
        is left at the default, and is added to the `docker` group for the
        unit only (Docker socket access is root-equivalent).
      '';
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "dmanager";
      description = "System group the service runs as.";
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 9283;
      description = "TCP port of the plain HTTP listener (`server.port`).";
    };

    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "";
      example = "127.0.0.1";
      description = ''
        IP address or host name the plain HTTP listener binds to
        (`server.listen_address`). Empty binds every interface.
      '';
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Open {option}`services.dmanager.port` on TCP. Has no effect while
        {option}`services.dmanager.http.enable` is false.
      '';
    };

    http.enable = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = ''
        Run the plain HTTP listener (`server.http.enabled`). May be turned off
        only when {option}`services.dmanager.tailscale.enable` is set:
        the process refuses to start with no listener.
      '';
    };

    webauthn = {
      rpId = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        example = "dmanager.<tailnet>.ts.net";
        description = "WebAuthn relying party ID (`webauthn.rp_id`).";
      };
      origins = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        example = [ "https://dmanager.<tailnet>.ts.net" ];
        description = "Allowed WebAuthn origins (`webauthn.origins`); required when `rpId` is set.";
      };
    };

    tailscale = {
      enable = lib.mkEnableOption "the embedded Tailscale node (`tailscale.enabled`, userspace networking)";
      hostname = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        example = "dmanager";
        description = "MagicDNS host name of the node (`tailscale.hostname`); null keeps the application default.";
      };
      httpsEnabled = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = "Serve the tailnet listener over HTTPS on 443 (`tailscale.https_enabled`).";
      };
      authKeyFile = secretFileOption "File holding the Tailscale auth key (`tailscale.auth_key_file`)." "/run/secrets/dmanager-tailscale-key";
    };

    smtp = {
      enable = lib.mkEnableOption "e-mail notifications through an SMTP relay (`smtp.enabled`)";
      host = lib.mkOption {
        type = lib.types.str;
        default = "";
        example = "smtp.example.com";
        description = "SMTP relay host name (`smtp.host`).";
      };
      port = lib.mkOption {
        type = lib.types.port;
        default = 25;
        description = "SMTP relay port (`smtp.port`).";
      };
      username = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "SMTP user name (`smtp.username`).";
      };
      passwordFile = secretFileOption "File holding the SMTP password (`smtp.password_file`)." "/run/secrets/dmanager-smtp";
      fromEmail = lib.mkOption {
        type = lib.types.str;
        default = "";
        example = "dmanager@example.com";
        description = "Sender address (`smtp.from_email`).";
      };
      fromName = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Sender display name (`smtp.from_name`).";
      };
      tlsMode = lib.mkOption {
        type = lib.types.enum [ "none" "starttls" "tls" ];
        default = "none";
        description = "Transport security towards the relay (`smtp.tls_mode`).";
      };
    };

    registries = lib.mkOption {
      default = [ ];
      description = "Private registry credentials (`registries`).";
      type = lib.types.listOf (
        lib.types.submodule {
          options = {
            host = lib.mkOption {
              type = lib.types.str;
              example = "registry.example.com";
              description = "Registry host name.";
            };
            username = lib.mkOption {
              type = lib.types.str;
              description = "Registry user name.";
            };
            passwordFile = secretFileOption "File holding the registry password (`password_file`)." "/run/secrets/dmanager-registry";
          };
        }
      );
    };

    externalLabelSelectors = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [ "com.example.pool=true" ];
      description = ''
        Label selectors (`key=value`, or a bare `key`) marking containers as
        externally managed (`docker.external_label_selectors`), in addition to
        the `dmanager.managed-by` label. Such containers are never upgraded.
      '';
    };

    notifyExternalUpdates = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Send update notifications for externally managed containers too (`scheduler.notify_external_updates`).";
    };

    settings = lib.mkOption {
      default = { };
      type = settingsFormat.type;
      example = lib.literalExpression ''
        {
          scheduler.interval_minutes = 30;
          auth.secure_cookies = "always";
        }
      '';
      description = ''
        Configuration in the YAML structure of the config file, rendered to
        the Nix store and passed as `--config`. The typed options above are
        merged into it with {function}`lib.mkDefault`, so a value set here
        wins. The rendered file is world-readable: secrets belong in the
        `*File` options or in {option}`services.dmanager.environmentFile`.
      '';
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr absolutePath;
      default = null;
      example = "/run/secrets/dmanager.env";
      description = ''
        Dotenv file loaded into the service's environment by systemd (read as
        root), for secrets given as `DMANAGER_*` variables. Store paths are
        rejected: a path literal would be copied into the world-readable Nix
        store.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = leakedKeys == [ ];
        message = ''
          services.dmanager.settings must not contain secret values (found:
          ${lib.concatStringsSep ", " leakedKeys}): the rendered config is
          world-readable in the Nix store. Use the *_file settings
          (services.dmanager.*.passwordFile / authKeyFile) or
          services.dmanager.environmentFile instead.
        '';
      }
      {
        assertion = config.virtualisation.docker.enable;
        message = "services.dmanager needs the Docker engine: set virtualisation.docker.enable = true.";
      }
    ];

    services.dmanager.settings = lib.mkMerge [
      {
        server = {
          port = lib.mkDefault (toString cfg.port);
          listen_address = lib.mkDefault cfg.listenAddress;
          http.enabled = lib.mkDefault cfg.http.enable;
          db_path = lib.mkDefault "${stateDir}/dmanager.db";
        };
        docker.external_label_selectors = lib.mkDefault cfg.externalLabelSelectors;
        scheduler.notify_external_updates = lib.mkDefault cfg.notifyExternalUpdates;
      }
      (optionalSet (cfg.webauthn.rpId != null) {
        webauthn = {
          rp_id = lib.mkDefault cfg.webauthn.rpId;
          origins = lib.mkDefault cfg.webauthn.origins;
        };
      })
      (optionalSet cfg.tailscale.enable {
        tailscale = {
          enabled = lib.mkDefault true;
          https_enabled = lib.mkDefault cfg.tailscale.httpsEnabled;
        }
        // optionalSet (cfg.tailscale.hostname != null) { hostname = lib.mkDefault cfg.tailscale.hostname; }
        // optionalSet (cfg.tailscale.authKeyFile != null) {
          auth_key_file = lib.mkDefault (toString cfg.tailscale.authKeyFile);
        };
      })
      (optionalSet cfg.smtp.enable {
        smtp = {
          enabled = lib.mkDefault true;
          host = lib.mkDefault cfg.smtp.host;
          port = lib.mkDefault (toString cfg.smtp.port);
          username = lib.mkDefault cfg.smtp.username;
          from_email = lib.mkDefault cfg.smtp.fromEmail;
          from_name = lib.mkDefault cfg.smtp.fromName;
          tls_mode = lib.mkDefault cfg.smtp.tlsMode;
        }
        // optionalSet (cfg.smtp.passwordFile != null) {
          password_file = lib.mkDefault (toString cfg.smtp.passwordFile);
        };
      })
      (optionalSet (cfg.registries != [ ]) {
        registries = lib.mkDefault (
          map (
            r:
            {
              inherit (r) host username;
            }
            // optionalSet (r.passwordFile != null) { password_file = toString r.passwordFile; }
          ) cfg.registries
        );
      })
    ];

    users.users = lib.mkIf (cfg.user == "dmanager") {
      dmanager = {
        isSystemUser = true;
        group = cfg.group;
        home = stateDir;
        description = "dmanager";
      };
    };
    users.groups = lib.mkIf (cfg.group == "dmanager") { dmanager = { }; };

    networking.firewall.allowedTCPPorts = lib.mkIf (cfg.openFirewall && cfg.http.enable) [ cfg.port ];

    systemd.services.dmanager = {
      description = "dmanager Docker container update manager";
      wantedBy = [ "multi-user.target" ];
      after = [ "docker.service" ];
      requires = [ "docker.service" ];
      environment.DMANAGER_ENV = "production";

      serviceConfig = {
        ExecStart = "${lib.getExe cfg.package} serve --config ${configFile}";
        EnvironmentFile = lib.mkIf (cfg.environmentFile != null) cfg.environmentFile;
        User = cfg.user;
        Group = cfg.group;
        # Scoped to the unit: the user itself stays out of `docker`. Socket
        # access is root-equivalent.
        SupplementaryGroups = [ "docker" ];

        StateDirectory = "dmanager";
        StateDirectoryMode = "0700";
        WorkingDirectory = stateDir;
        UMask = "0077";

        Restart = "on-failure";
        RestartSec = "5s";
        # An invalid Tailscale auth key exits on purpose; back off so the
        # rejected key is not resent to the control server every 5s forever.
        RestartSteps = 10;
        RestartMaxDelaySec = "5min";

        # Hardening. It limits what a compromised process can do to the host
        # directly; it cannot limit what it can do through the Docker socket.
        # The Docker socket needs AF_UNIX; the tsnet node needs the IP
        # families and AF_NETLINK (network monitor) and runs in userspace.
        # Everything but the state directory is read-only, and `/run` stays
        # readable for secret files.
        NoNewPrivileges = true;
        CapabilityBoundingSet = "";
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectKernelLogs = true;
        ProtectControlGroups = true;
        ProtectClock = true;
        ProtectHostname = true;
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        SystemCallArchitectures = "native";
        SystemCallFilter = [ "@system-service" ];
        RestrictAddressFamilies = [
          "AF_UNIX"
          "AF_INET"
          "AF_INET6"
          "AF_NETLINK"
        ];
      };
    };
  };
}
