"""Validate the public deployment configuration without a Docker daemon."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import sys
import shutil
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


def render(env_file, success=True, all_profiles=False):
    # CI project/port overrides should not change the sample contract check.
    environment = {k: v for k, v in os.environ.items() if k not in values(EXAMPLE)}
    result = subprocess.run(
        ["docker", "compose", "--project-directory", str(ROOT),
         "--file", str(ROOT / "compose.yaml"), "--env-file", str(env_file),
         *(["--profile", "*"] if all_profiles else []), "config", "--format", "json"],
        env=environment, capture_output=True, text=True, check=False,
    )
    if not success:
        assert result.returncode != 0, "Missing required configuration was accepted"
        return
    assert result.returncode == 0, result.stderr
    return json.loads(result.stdout)


def check_modes(env_file):
    render(EXAMPLE, success=False)
    env_file.write_text(fixture_text("production"), encoding="utf-8")
    production = render(env_file)
    assert production["name"] == "sicp-production"
    services = production["services"]
    assert set(services) == {"db-m", "db-s", "db-log", "redis", "volumes", "migrate", "query", "worker", "console"}
    assert services["worker"].get("depends_on", {}).get("mock", {}).get("required") is False
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


def main(directory):
    env_file = Path(directory) / ".env"
    config = check_modes(env_file)
    services = config["services"]
    assert config["name"] == "sicp-development"
    assert set(services) == {
        "db-m", "db-s", "db-log", "redis", "volumes", "mock",
        "migrate", "query", "worker", "console", "mock-volumes",
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
        "DATABASE_M0_URL", "DATABASE_M1_URL", "DATABASE_S0_URL",
        "DATABASE_S1_URL", "DATABASE_CONTROL_URL", "DATABASE_LOG_URL",
        "DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_LOG_USER", "DATABASE_LOG_PASSWORD",
    }
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
        data_volumes = [v for v in services[name]["volumes"] if v["type"] == "volume"]
        assert len(data_volumes) == 1, f"{name} must have one persistent database volume"
        assert data_volumes[0]["source"] == "pg-" + name.removeprefix("db-")
        assert data_volumes[0]["target"] == "/var/lib/postgresql"
        assert not data_volumes[0].get("read_only", False)
        assert services[name]["environment"]["SICP_CLUSTER"] == cluster
        assert services[name]["environment"]["POSTGRES_USER"] == "postgres"
        assert "127.0.0.1" in services[name]["healthcheck"]["test"][1]
    for role in ("query", "worker"):
        assert services["console"]["depends_on"][role]["condition"] == "service_healthy"
    assert Path(services["console"]["build"]["context"]) == ROOT / "frontend"
    assert Path(services["mock"]["build"]["context"]) == ROOT / "deploy"

    with tempfile.TemporaryDirectory() as directory:
        env_file = Path(directory) / ".env"
        env_file.write_text(fixture_text("production") + "\n"
                            "SICP_PROJECT=compose-config-test\n"
                            "SICP_CONSOLE_PORT=28082\n"
                            "QUERY_API_KEY='literal-$value-#-secret'\n"
                            "SOURCE_APP_SECRET='app-@:/?#%$value'\n"
                            "POSTGRES_USER=custom_admin\n"
                            "POSTGRES_PASSWORD='admin-@:/?#%$value'\n"
                            "QUERY_PASSWORD='query-@:/?#%$value'\n"
                            "WORKER_PASSWORD='worker-@:/?#%$value'\n"
                            "LOGGER_PASSWORD='logger-@:/?#%$value'\n", encoding="utf-8")
        custom = render(env_file)
        assert custom["name"] == "compose-config-test"
        assert custom["services"]["console"]["ports"][0]["published"] == "28082"
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
    print("Production/development selection, source/mail isolation and password inheritance passed.")


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--write-development-env":
        with Path(sys.argv[2]).open("x", encoding="utf-8") as output:
            output.write(fixture_text("development"))
    else:
        with tempfile.TemporaryDirectory() as directory:
            main(directory)
