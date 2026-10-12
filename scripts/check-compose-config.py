"""Validate the public deployment configuration without a Docker daemon."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import sys
import shutil
from gateway_runtime import GatewayConfig
from urllib.parse import urlsplit, urlunsplit


ROOT = Path(__file__).resolve().parent.parent
EXAMPLE = ROOT / ".env.example"


def values(path):
    return dict(
        line.split("=", 1)
        for line in path.read_text(encoding="utf-8").splitlines()
        if line and not line.startswith("#")
    )


def fixture_text(mode):
    query = values(ROOT / "internal/assembly/testdata/deployment/query.env")
    worker = values(ROOT / "internal/assembly/testdata/deployment/worker.env")
    overrides = {
        "ENVIRONMENT": mode,
        "POSTGRES_PASSWORD": "synthetic", "QUERY_PASSWORD": "synthetic",
        "WORKER_PASSWORD": "synthetic", "LOGGER_PASSWORD": "synthetic",
        "QUERY_API_KEY": query["QUERY_API_KEY"], "SNAPSHOT_KEY": query["SNAPSHOT_KEY"],
        "AUDIT_KEY": query["AUDIT_KEY"],
        "ADMIN_USERNAME": worker["ADMIN_USERNAME"],
        "ADMIN_PASSWORD": worker["ADMIN_PASSWORD"],
    }
    if mode == "production":
        overrides.update({
            "SOURCE_ORIGIN": "https://upstream.example.invalid",
            "SOURCE_APP_ID": "fixture-app-id", "SOURCE_APP_SECRET": "fixture-app-secret", "SOURCE_SCOPE": "fixture-school",
            "MAIL_ADDRESS": "smtp.example.invalid:587", "MAIL_TLS": "starttls",
            "MAIL_FROM": "worker@example.invalid", "MAIL_TO": "operator@example.invalid",
            "MAIL_USERNAME": "fixture-mail-user", "MAIL_PASSWORD": "fixture-mail-password",
        })
    else:
        # Integration fixtures explicitly opt in to synthetic mail delivery.
        overrides["MAIL_ADDRESS"] = "mock-mail:1025"
    return "\n".join(
        f"{key}={overrides[key]}" if (key := line.split("=", 1)[0]) in overrides else line
        for line in EXAMPLE.read_text(encoding="utf-8").splitlines()
    ) + "\n"


def entrypoint(environment, success=True):
    shell = os.environ.get("SICP_TEST_BASH") or shutil.which("bash")
    assert shell, "Bash is required to check worker mode selection"
    variables = os.environ.copy()
    # Docker config doubles dollars only in its reusable rendered representation.
    variables.update({key: value.replace("$$", "$") for key, value in environment.items()})
    result = subprocess.run(
        [shell, str(ROOT / "deploy/worker-entrypoint.sh"), "env"],
        env=variables, capture_output=True, text=True, check=False,
    )
    if not success:
        assert result.returncode != 0, "Invalid production source/mail configuration accepted"
        return
    assert result.returncode == 0, result.stderr
    return dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)


def render(env_file, success=True, all_profiles=False, project=None):
    # CI project/port overrides should not change the sample contract check.
    environment = {k: v for k, v in os.environ.items() if k not in values(EXAMPLE) and k != "COMPOSE_PROJECT_NAME"}
    result = subprocess.run(
        ["docker", "compose", "--project-directory", str(ROOT),
         "--file", str(ROOT / "compose.yaml"), "--env-file", str(env_file),
         *(["--profile", "*"] if all_profiles else []),
         *(["--project-name", project] if project else []), "config", "--format", "json"],
        env=environment, capture_output=True, text=True, check=False,
    )
    if not success:
        assert result.returncode != 0, "Missing required configuration was accepted"
        return
    assert result.returncode == 0, result.stderr
    return json.loads(result.stdout)


def check_modes(env_file):
    for mode in ('production', 'development'):
        for tls in (False, True):
            text = fixture_text(mode)
            if tls:
                text += "\nSICP_TLS_CERT_FILE='/synthetic/a space/$$literal.pem'\nSICP_TLS_KEY_FILE='/synthetic/key.pem'\n"
            env_file.write_text(text, encoding='utf-8')
            rendered = render(env_file)
            # Decode Compose's serialization escaping exactly once.
            config = GatewayConfig.from_model(json.loads(json.dumps(rendered).replace('$$', '$')))
            assert config.mode == ('https' if tls else 'http')
            assert entrypoint(rendered['services']['worker']['environment'])['CONSOLE_COOKIE_SECURE'] == ('true' if tls else 'false')
            if tls:
                assert config.cert == '/synthetic/a space/$$literal.pem'
    render(EXAMPLE, success=False)
    env_file.write_text(fixture_text("production"), encoding="utf-8")
    production = render(env_file)
    assert production["name"] == "sicp-production"
    services = production["services"]
    assert set(services) == {"db-m", "db-s", "db-log", "redis", "volumes", "migrate", "query", "query-gateway", "worker", "console"}
    assert services["worker"].get("depends_on", {}).get("mock", {}).get("required") is False
    assert services["query"]["deploy"]["replicas"] == 1
    assert not services["query"].get("ports")
    assert "container_name" not in services["query"]
    assert services["query-gateway"]["ports"][0]["published"] == "18080"
    assert services["query-gateway"]["depends_on"]["query"]["condition"] == "service_healthy"
    assert services["worker"]["environment"]["CONSOLE_QUERY_URL"] == "http://query-gateway:8080"
    assert "QUERY_REPLICAS" not in services["worker"]["environment"]
    for count in (2, 4):
        env_file.write_text(fixture_text("production").replace("QUERY_REPLICAS=1", f"QUERY_REPLICAS={count}"), encoding="utf-8")
        scaled = render(env_file)["services"]
        assert scaled["query"]["deploy"]["replicas"] == count
        assert scaled["query-gateway"]["environment"]["QUERY_REPLICAS"] == str(count)
    env_file.write_text(fixture_text("production").replace("QUERY_REPLICAS=1\n", ""), encoding="utf-8")
    assert render(env_file)["services"]["query"]["deploy"]["replicas"] == 1
    pg_limits = {
        "db-m": ("POSTGRES_M_MAX_CONNECTIONS", "256", "320"),
        "db-s": ("POSTGRES_S_MAX_CONNECTIONS", "128", "160"),
        "db-log": ("POSTGRES_LOG_MAX_CONNECTIONS", "100", "120"),
    }
    for mode in ("production", "development"):
        baseline = fixture_text(mode)
        for variant in ("default", "custom", "omitted", "blank"):
            text = baseline
            for key, default, custom in pg_limits.values():
                replacement = {"default": f"{key}={default}\n", "custom": f"{key}={custom}\n",
                               "omitted": "", "blank": f"{key}=\n"}[variant]
                text = text.replace(f"{key}={default}\n", replacement)
            env_file.write_text(text, encoding="utf-8")
            configured = render(env_file)["services"]
            for service, (key, default, custom) in pg_limits.items():
                expected = custom if variant == "custom" else "" if variant == "blank" else default
                assert configured[service]["command"] == ["postgres", "-c", f"max_connections={expected}"]
                for role in ("query", "worker", "migrate"):
                    assert key not in configured[role]["environment"]
    capacity = {
        "QUERY_MAX_EXECUTIONS": ("8", "32"),
        "QUERY_MAX_QUEUE": ("64", "0"),
        "QUERY_EXECUTION_TIMEOUT": ("30s", "45s"),
        "QUERY_DB_BUSINESS_MAX_CONNS": ("16", "40"),
        "QUERY_DB_CONTROL_MAX_CONNS": ("16", "48"),
        "QUERY_DB_LOG_MAX_CONNS": ("16", "12"),
    }
    custom = fixture_text("production")
    omitted = custom
    for key, (default, override) in capacity.items():
        assert services["query"]["environment"][key] == default
        assert key not in services["worker"]["environment"]
        custom = custom.replace(f"{key}={default}\n", f"{key}={override}\n")
        omitted = omitted.replace(f"{key}={default}\n", "")
    for text, index in ((custom, 1), (omitted, 0)):
        env_file.write_text(text, encoding="utf-8")
        configured = render(env_file)["services"]
        for key, pair in capacity.items():
            assert configured["query"]["environment"][key] == pair[index]
    blank = fixture_text("production")
    for key, (default, _) in capacity.items():
        blank = blank.replace(f"{key}={default}\n", f"{key}=\n")
    env_file.write_text(blank, encoding="utf-8")
    configured = render(env_file)["services"]
    assert all(configured["query"]["environment"][key] == "" for key in capacity)
    env_file.write_text(fixture_text("production"), encoding="utf-8")
    for service in services.values():
        assert not any("mock" in volume.get("source", "") for volume in service.get("volumes", []))
    runtime = entrypoint(services["worker"]["environment"])
    quota_keys = ("STAGING_MAX_BYTES", "TARGET_MAX_BYTES", "SPOOL_MAX_BYTES")
    assert all(runtime[key] == "0" for key in quota_keys)
    no_quotas = fixture_text("production")
    for key in quota_keys:
        no_quotas = no_quotas.replace(f"{key}=0\n", "")
    env_file.write_text(no_quotas, encoding="utf-8")
    optional = render(env_file)["services"]["worker"]["environment"]
    assert all(optional[key] == "0" for key in quota_keys)
    env_file.write_text(fixture_text("production").replace("STAGING_MAX_BYTES=0", "STAGING_MAX_BYTES=268435456"), encoding="utf-8")
    assert render(env_file)["services"]["worker"]["environment"]["STAGING_MAX_BYTES"] == "268435456"
    assert runtime["SOURCE_PAGE_SIZE"] == "1000"
    assert runtime["SOURCE_PAGE_CONCURRENCY"] == "4"
    env_file.write_text(fixture_text("production").replace("SOURCE_PAGE_CONCURRENCY=4", "SOURCE_PAGE_CONCURRENCY=2"), encoding="utf-8")
    assert render(env_file)["services"]["worker"]["environment"]["SOURCE_PAGE_CONCURRENCY"] == "2"
    env_file.write_text(fixture_text("production").replace("SOURCE_PAGE_CONCURRENCY=4\n", ""), encoding="utf-8")
    assert render(env_file)["services"]["worker"]["environment"]["SOURCE_PAGE_CONCURRENCY"] == "4"
    env_file.write_text(fixture_text("production").replace("SOURCE_PAGE_SIZE=1000", "SOURCE_PAGE_SIZE=5000"), encoding="utf-8")
    assert render(env_file)["services"]["worker"]["environment"]["SOURCE_PAGE_SIZE"] == "5000"
    env_file.write_text(fixture_text("production").replace("SOURCE_PAGE_SIZE=1000\n", ""), encoding="utf-8")
    assert render(env_file)["services"]["worker"]["environment"]["SOURCE_PAGE_SIZE"] == "1000"
    env_file.write_text(fixture_text("production"), encoding="utf-8")
    assert "SOURCE_TOKEN" not in runtime
    assert "SOURCE_APP_SECRET" not in services["query"]["environment"]
    for key in ("SOURCE_ORIGIN", "SOURCE_APP_ID", "SOURCE_APP_SECRET", "SOURCE_SCOPE", "MAIL_ADDRESS", "MAIL_TLS", "MAIL_USERNAME", "MAIL_PASSWORD"):
        assert runtime[key] == services["worker"]["environment"][key]
    for missing in ("SOURCE_ORIGIN", "SOURCE_APP_ID", "SOURCE_APP_SECRET", "SOURCE_SCOPE", "MAIL_ADDRESS", "MAIL_FROM", "MAIL_TO"):
        entrypoint({**services["worker"]["environment"], missing: ""}, success=False)
    for address in ("http://mock-source:8090", "http://mock-source", "https://MOCK-SOURCE/"):
        entrypoint({**services["worker"]["environment"], "SOURCE_ORIGIN": address}, success=False)
    entrypoint({**services["worker"]["environment"], "MAIL_ADDRESS": "mock-mail:1025"}, success=False)
    entrypoint({**services["worker"]["environment"], "ENVIRONMENT": "typo"}, success=False)
    ignored = entrypoint({**services["worker"]["environment"], "ENVIRONMENT": "development"})
    assert ignored["SOURCE_ORIGIN"] == "http://mock-source:8090"
    assert ignored["SOURCE_APP_ID"] == ignored["SOURCE_APP_SECRET"] == ""
    assert ignored["SOURCE_TOKEN"] == "synthetic-source-token"
    assert ignored["MAIL_ADDRESS"] == "mock-mail:1025"
    assert ignored["MAIL_USERNAME"] == ignored["MAIL_PASSWORD"] == ""
    mail_keys = ("MAIL_ADDRESS", "MAIL_TLS", "MAIL_FROM", "MAIL_TO", "MAIL_USERNAME", "MAIL_PASSWORD")
    assert all(values(EXAMPLE)[key] == "" for key in mail_keys)
    for mode in ("production", "staging", "test", "development"):
        disabled = {**services["worker"]["environment"], "ENVIRONMENT": mode, **dict.fromkeys(mail_keys, "")}
        assert all(entrypoint(disabled).get(key, "") == "" for key in mail_keys)
        for key in mail_keys:
            disabled.pop(key)
        assert all(entrypoint(disabled).get(key, "") == "" for key in mail_keys)
        if mode != "development":
            for key in mail_keys:
                entrypoint({**disabled, key: services["worker"]["environment"][key]}, success=False)
    env_file.write_text(fixture_text("development"), encoding="utf-8")
    development = render(env_file)
    assert development["services"]["mock"]["profiles"] == ["development"]
    assert development["services"]["worker"]["depends_on"]["mock"]["condition"] == "service_healthy"
    return development


def check_host_mounts(env_file):
    # One root controls every business mount; application paths remain fixed.
    bindings = {
        "pg-m": [("db-m", "/var/lib/postgresql")],
        "pg-s": [("db-s", "/var/lib/postgresql")],
        "pg-log": [("db-log", "/var/lib/postgresql")],
        "redis": [("redis", "/data")],
        "query-logs": [("query", "/var/lib/sicp/logs"), ("volumes", "/query-logs")],
        "query-replicas": [("query", "/var/lib/sicp/query-replicas"), ("volumes", "/query-replicas")],
        "worker-logs": [("worker", "/var/lib/sicp/logs"), ("volumes", "/worker-logs")],
        "staging": [("worker", "/var/lib/sicp/staging"), ("volumes", "/staging")],
        "targets": [("worker", "/var/lib/sicp/targets"), ("volumes", "/targets")],
        "spool": [("worker", "/var/lib/sicp/spool"), ("volumes", "/spool")],
    }
    custom = fixture_text("development").replace("SICP_DATA_ROOT=/home/ubuntu/sicp", "SICP_DATA_ROOT=/custom-storage")
    env_file.write_text(custom, encoding="utf-8")
    config = render(env_file)
    assert set(config.get("volumes", {})) == {"mock-mail"}
    for service, target in (("mock", "/mail"), ("mock-volumes", "/mock-mail")):
        mount, = [v for v in config["services"][service]["volumes"] if v["target"] == target]
        assert mount["type"] == "volume" and mount["source"] == "mock-mail"
    for folder, consumers in bindings.items():
        for service, target in consumers:
            mount, = [v for v in config["services"][service]["volumes"] if v["target"] == target]
            assert mount["type"] == "bind"
            assert mount["source"].replace("\\", "/").endswith(f"/custom-storage/sicp-development/{folder}")
            assert not mount.get("read_only", False)
    for service in ("query", "worker"):
        assert config["services"][service]["environment"]["LOG_DIRECTORY"] == "/var/lib/sicp/logs"
    for key, directory in (("STAGING_DIRECTORY", "staging"), ("TARGET_DIRECTORY", "targets"), ("SPOOL_DIRECTORY", "spool")):
        assert config["services"]["worker"]["environment"][key] == "/var/lib/sicp/" + directory
    env_file.write_text(custom.replace("SICP_PROJECT=sicp-${ENVIRONMENT}", "SICP_PROJECT=isolated-project"), encoding="utf-8")
    isolated = render(env_file)
    for folder, consumers in bindings.items():
        for service, target in consumers:
            mount, = [v for v in isolated["services"][service]["volumes"] if v["target"] == target]
            assert mount["source"].replace("\\", "/").endswith(f"/custom-storage/isolated-project/{folder}")
    # CLI project overrides must isolate mounts even when SICP_PROJECT differs.
    env_file.write_text(custom, encoding="utf-8")
    cli_project = render(env_file, project="cli-isolated-project")
    assert cli_project["name"] == "cli-isolated-project"
    for folder, consumers in bindings.items():
        for service, target in consumers:
            mount, = [v for v in cli_project["services"][service]["volumes"] if v["target"] == target]
            assert mount["source"].replace("\\", "/").endswith(f"/custom-storage/cli-isolated-project/{folder}")
    env_file.write_text(custom.replace("SICP_DATA_ROOT=/custom-storage", "SICP_DATA_ROOT="), encoding="utf-8")
    render(env_file, success=False)


def main(directory):
    env_file = Path(directory) / ".env"
    config = check_modes(env_file)
    services = config["services"]
    assert config["name"] == "sicp-development"
    assert set(services) == {
        "db-m", "db-s", "db-log", "redis", "volumes", "mock",
        "migrate", "query", "query-gateway", "worker", "console", "mock-volumes",
    }
    # These fixtures also reach actual Go assembly in the unit/integration tests.
    for role in ("query", "worker"):
        expected = values(ROOT / "internal/assembly/testdata/deployment" / f"{role}.env")
        for key in tuple(expected):
            if key.startswith("DATABASE_"):
                endpoint = urlsplit(expected[key])
                prefix = "DATABASE_LOG_" if key == "DATABASE_LOG_URL" else "DATABASE_"
                expected[prefix + "USER"] = endpoint.username
                expected[prefix + "PASSWORD"] = endpoint.password
                expected[key] = urlunsplit(endpoint._replace(netloc=endpoint.netloc.split("@", 1)[1]))
        expected["ENVIRONMENT"] = "development"
        actual = entrypoint(services[role]["environment"]) if role == "worker" else services[role]["environment"]
        for key, value in expected.items():
            assert actual[key] == value, f"{role} runtime contract drift: {key}"
        for dependency in ("volumes", "migrate"):
            assert services[role]["depends_on"][dependency]["condition"] == "service_completed_successfully"
        assert Path(services[role]["build"]["context"]) == ROOT
    assert "POSTGRES_PASSWORD" not in services["query"]["environment"]
    assert "ADMIN_USERNAME" not in services["query"]["environment"]
    assert "ADMIN_PASSWORD" not in services["query"]["environment"]
    assert set(services["migrate"]["environment"]) == {
        "LOG_STORAGE_MODE",
        "DATABASE_M0_URL", "DATABASE_M1_URL", "DATABASE_S0_URL",
        "DATABASE_S1_URL", "DATABASE_CONTROL_URL", "DATABASE_LOG_URL",
        "DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_LOG_USER", "DATABASE_LOG_PASSWORD",
    }
    for role in ("query", "worker", "migrate"):
        assert services[role]["environment"]["LOG_STORAGE_MODE"] == "single_legacy"
    all_services = render(env_file, all_profiles=True)["services"]
    assert "ctl" not in all_services
    assert services["migrate"]["entrypoint"] == ["/usr/local/bin/db-migrate"]
    assert services["migrate"]["restart"] == "no"
    assert "MANAGEMENT_URL" not in values(EXAMPLE)
    for service in all_services.values():
        assert "tools" not in service.get("profiles", [])
        assert "MANAGEMENT_URL" not in service.get("environment", {})
    for role in ("query", "worker"):
        assert services[role]["healthcheck"]["test"] == ["CMD", "/usr/local/bin/service", "--healthcheck"]
        assert services[role]["environment"]["DATABASE_USER"] != services["migrate"]["environment"]["DATABASE_USER"]
    for name, cluster in (("db-m", "M"), ("db-s", "S"), ("db-log", "log")):
        # Keep deployment aligned with the P01 database decision and PG 18 layout.
        assert services[name]["image"] == "postgres:18.6-trixie", f"{name} database version drift"
        data_volumes = [v for v in services[name]["volumes"] if v["target"] == "/var/lib/postgresql"]
        assert len(data_volumes) == 1, f"{name} must have one persistent database volume"
        assert data_volumes[0]["type"] == "bind"
        assert data_volumes[0]["source"].replace("\\", "/").endswith("/sicp-development/pg-" + name.removeprefix("db-"))
        assert data_volumes[0]["target"] == "/var/lib/postgresql"
        assert not data_volumes[0].get("read_only", False)
        assert services[name]["environment"]["SICP_CLUSTER"] == cluster
        assert services[name]["environment"]["POSTGRES_USER"] == "postgres"
        assert "127.0.0.1" in services[name]["healthcheck"]["test"][1]
    for role in ("query-gateway", "worker"):
        assert services["console"]["depends_on"][role]["condition"] == "service_healthy"
    assert Path(services["console"]["build"]["context"]) == ROOT / "frontend"
    assert Path(services["mock"]["build"]["context"]) == ROOT / "deploy"

    with tempfile.TemporaryDirectory() as directory:
        env_file = Path(directory) / ".env"
        env_file.write_text(fixture_text("production") + "\n"
                            "SICP_PROJECT=compose-config-test\n"
                            "SICP_GATEWAY_PORT=28082\n"
                            "QUERY_API_KEY='literal-$value-#-secret'\n"
                            "SOURCE_APP_SECRET='app-@:/?#%$value'\n"
                            "POSTGRES_USER=custom_admin\n"
                            "POSTGRES_PASSWORD='admin-@:/?#%$value'\n"
                            "QUERY_PASSWORD='query-@:/?#%$value'\n"
                            "WORKER_PASSWORD='worker-@:/?#%$value'\n"
                            "LOGGER_PASSWORD='logger-@:/?#%$value'\n", encoding="utf-8")
        custom = render(env_file)
        assert custom["name"] == "compose-config-test"
        assert custom["services"]["query-gateway"]["ports"][0]["published"] == "28082"
        assert entrypoint(custom["services"]["worker"]["environment"])["SOURCE_APP_SECRET"] == "app-@:/?#%$value"
        # Compose escapes dollars when rendering reusable configuration.
        for role in ("query", "worker"):
            assert custom["services"][role]["environment"]["QUERY_API_KEY"] == "literal-$$value-#-secret"
        # A single edit must feed bootstrap and every consumer without exposing
        # another role's password or inserting raw credentials into endpoint URLs.
        endpoints = {k for k in values(EXAMPLE) if k.startswith("DATABASE_")}
        for role, password in (("query", "query"), ("worker", "worker"), ("migrate", "admin")):
            runtime = custom["services"][role]["environment"]
            assert runtime["DATABASE_PASSWORD"] == password + "-@:/?#%$$value"
            assert runtime["DATABASE_LOG_PASSWORD"] == ("admin" if role == "migrate" else "logger") + "-@:/?#%$$value"
            assert runtime["DATABASE_USER"] == ("custom_admin" if role == "migrate" else "sicp_" + role)
            for key in endpoints:
                assert runtime[key] == services[role]["environment"][key]
                assert urlsplit(runtime[key]).username is None
            if role == "query":
                assert not any(v in runtime.values() for v in ("admin-@:/?#%$$value", "worker-@:/?#%$$value"))
        for name in ("db-m", "db-s", "db-log"):
            bootstrap = custom["services"][name]["environment"]
            assert bootstrap["POSTGRES_USER"] == "custom_admin"
            for key, prefix in (("POSTGRES_PASSWORD", "admin"), ("QUERY_PASSWORD", "query"), ("WORKER_PASSWORD", "worker"), ("LOGGER_PASSWORD", "logger")):
                assert bootstrap[key] == prefix + "-@:/?#%$$value"
        env_file.write_text("QUERY_API_KEY=\n", encoding="utf-8")
        render(env_file, success=False)
    with tempfile.TemporaryDirectory() as mount_directory:
        check_host_mounts(Path(mount_directory) / "host-mounts.env")
    print("Production/development selection, source/mail isolation, password inheritance and host mounts passed.")


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--write-development-env":
        with Path(sys.argv[2]).open("x", encoding="utf-8") as output:
            output.write(fixture_text("development"))
    else:
        with tempfile.TemporaryDirectory() as directory:
            main(directory)
