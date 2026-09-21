"""Print one lossless source response, useful for curl-free collector tests."""

import argparse

from .source import ROOT, TABLES, MockSource, dumps


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=ROOT)
    parser.add_argument("--snapshot", choices=("initial", "updated", "after_delete"), default="initial")
    parser.add_argument("--table", required=True, choices=TABLES)
    parser.add_argument("--page", type=int, default=1)
    parser.add_argument("--page-size", type=int, default=5)
    parser.add_argument("--start-time")
    parser.add_argument("--end-time")
    args = parser.parse_args()
    body = MockSource(args.snapshot, args.root).request(args.table, page=args.page, pageSize=args.page_size,
        startTime=args.start_time, endTime=args.end_time)
    print(dumps(body), end="")


if __name__ == "__main__":
    main()
