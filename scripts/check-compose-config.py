"""Validate the public deployment configuration without a Docker daemon."""
import json
import os
from pathlib import Path
import subprocess
import tempfile


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
                            "QUERY_API_KEY='literal-$value-#-secret'\n", encoding="utf-8")
        custom = render(env_file)
        assert custom["name"] == "compose-config-test"
        assert custom["services"]["console"]["ports"][0]["published"] == "28082"
        # Compose escapes dollars when rendering reusable configuration.
        for role in ("query", "worker"):
            assert custom["services"][role]["environment"]["QUERY_API_KEY"] == "literal-$$value-#-secret"
        env_file.write_text("QUERY_API_KEY=\n", encoding="utf-8")
        render(env_file, success=False)
    print("Unified Compose configuration, service isolation and overrides passed.")


if __name__ == "__main__":
    main()
