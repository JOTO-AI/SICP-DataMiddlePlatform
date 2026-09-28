"""Validate the public deployment configuration without a Docker daemon."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
from urllib.parse import urlsplit, urlunsplit


ROOT = Path(__file__).resolve().parent.parent
EXAMPLE = ROOT / ".env.example"


def values(path):
    return dict(
        line.split("=", 1)
        for line in path.read_text(encoding="utf-8").splitlines()
        if line and not line.startswith("#")
    )


def render(env_file, success=True):
    # CI project/port overrides should not change the sample contract check.
    environment = {k: v for k, v in os.environ.items() if k not in values(EXAMPLE)}
    result = subprocess.run(
        ["docker", "compose", "--project-directory", str(ROOT),
         "--file", str(ROOT / "compose.yaml"), "--env-file", str(env_file),
         "--profile", "tools", "config", "--format", "json"],
        env=environment, capture_output=True, text=True, check=False,
    )
    if not success:
        assert result.returncode != 0, "Missing required configuration was accepted"
        return
    assert result.returncode == 0, result.stderr
    return json.loads(result.stdout)


def main():
    config = render(EXAMPLE)
    services = config["services"]
    assert config["name"] == "sicp-phase1"
    assert set(services) == {
        "db-m", "db-s", "db-log", "redis", "volumes", "mock",
        "migrate", "query", "worker", "console", "ctl",
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
        assert services[role]["environment"] == expected, f"{role} runtime contract drift"
        for dependency in ("volumes", "migrate"):
            assert services[role]["depends_on"][dependency]["condition"] == "service_completed_successfully"
        assert Path(services[role]["build"]["context"]) == ROOT
    assert "POSTGRES_PASSWORD" not in services["query"]["environment"]
    assert "MANAGEMENT_TOKEN" not in services["query"]["environment"]
    assert "MANAGEMENT_CREDENTIALS" not in services["query"]["environment"]
    assert set(services["migrate"]["environment"]) == {
        "DATABASE_M0_URL", "DATABASE_M1_URL", "DATABASE_S0_URL",
        "DATABASE_S1_URL", "DATABASE_CONTROL_URL", "DATABASE_LOG_URL",
        "DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_LOG_USER", "DATABASE_LOG_PASSWORD",
    }
    assert set(services["ctl"]["environment"]) == {"MANAGEMENT_URL", "MANAGEMENT_TOKEN"}
    for name, cluster in (("db-m", "M"), ("db-s", "S"), ("db-log", "log")):
        assert services[name]["environment"]["SICP_CLUSTER"] == cluster
        assert services[name]["environment"]["POSTGRES_USER"] == "postgres"
        assert "127.0.0.1" in services[name]["healthcheck"]["test"][1]
    for role in ("query", "worker"):
        assert services["console"]["depends_on"][role]["condition"] == "service_healthy"
    assert Path(services["console"]["build"]["context"]) == ROOT / "frontend"
    assert Path(services["mock"]["build"]["context"]) == ROOT / "deploy"

    with tempfile.TemporaryDirectory() as directory:
        env_file = Path(directory) / ".env"
        env_file.write_text(EXAMPLE.read_text(encoding="utf-8") + "\n"
                            "SICP_PROJECT=compose-config-test\n"
                            "SICP_CONSOLE_PORT=28082\n"
                            "QUERY_API_KEY='literal-$value-#-secret'\n"
                            "POSTGRES_USER=custom_admin\n"
                            "POSTGRES_PASSWORD='admin-@:/?#%$value'\n"
                            "QUERY_PASSWORD='query-@:/?#%$value'\n"
                            "WORKER_PASSWORD='worker-@:/?#%$value'\n"
                            "LOGGER_PASSWORD='logger-@:/?#%$value'\n", encoding="utf-8")
        custom = render(env_file)
        assert custom["name"] == "compose-config-test"
        assert custom["services"]["console"]["ports"][0]["published"] == "28082"
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
    print("Unified Compose configuration, service isolation and overrides passed.")


if __name__ == "__main__":
    main()
