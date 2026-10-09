"""Query one page of student grades using only the Python standard library."""
import argparse
from http.client import HTTPException
import json
import os
from pathlib import Path
import re
import sys
import time
import urllib.error
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def api_key(env_file):
    """Read the key from the environment or a single-line dotenv assignment."""
    if os.environ.get("QUERY_API_KEY"):
        return os.environ["QUERY_API_KEY"]
    for line in env_file.read_text(encoding="utf-8-sig").splitlines():
        name, separator, value = line.partition("=")
        if separator and name.strip() == "QUERY_API_KEY":
            value = value.strip()
            if value.startswith(("'", '"')):
                quote = value[0]
                end = value.find(quote, 1)
                if end < 0:
                    raise ValueError("Unclosed QUERY_API_KEY quote")
                return value[1:end]
            return re.split(r"\s+#", value, maxsplit=1)[0].strip()
    raise ValueError("QUERY_API_KEY is missing")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:18080")
    parser.add_argument("--student-number", required=True)
    parser.add_argument("--year", default="2025-2026")
    parser.add_argument("--semester", choices=("1", "2"))
    parser.add_argument("--page", type=int, default=1)
    parser.add_argument("--page-size", type=int, default=100)
    parser.add_argument("--env-file", type=Path,
                        default=Path(__file__).resolve().parents[1] / ".env")
    args = parser.parse_args()
    if args.page < 1 or not 1 <= args.page_size <= 1000:
        parser.error("page must be positive; page-size must be between 1 and 1000")
    try:
        key = api_key(args.env_file)
        if not key:
            raise ValueError("QUERY_API_KEY is empty")
    except (OSError, ValueError) as error:
        print(f"Cannot load QUERY_API_KEY ({type(error).__name__}). Check --env-file.",
              file=sys.stderr)
        return 1

    payload = {
        "student_number": args.student_number,
        "academic_year": args.year,
        "page": args.page,
        "page_size": args.page_size,
    }
    if args.semester:
        payload["semester"] = args.semester
    url = args.base_url.rstrip("/") + "/api/v1/sicp/students/grades/query"
    request = urllib.request.Request(
        url, data=json.dumps(payload).encode("utf-8"), method="POST",
        headers={"Content-Type": "application/json", "X-API-Key": key},
    )
    # Direct connection; never forward the API key through redirects.
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    started = time.perf_counter()
    try:
        try:
            response = client.open(request, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            status = response.status
            request_id = response.headers.get("X-Request-ID", "")
            result = json.load(response)
    except (OSError, ValueError, HTTPException) as error:
        print(f"Request failed ({type(error).__name__}). Check address, port and service.",
              file=sys.stderr)
        return 1
    elapsed = time.perf_counter() - started
    print(f"HTTP {status} | {elapsed:.3f}s | request_id={request_id}")
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if status == 200 and isinstance(result, dict) and result.get("code") == "ok" else 1


if __name__ == "__main__":
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8")
    sys.exit(main())
