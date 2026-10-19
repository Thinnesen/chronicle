"""Build a fan-out workflow. Print it as JSON, or submit it with --server.

uv run examples/fanout.py | kubectl create -n team-a -f -
uv run examples/fanout.py --server https://chronicle.example.com --namespace team-a --token-file token
"""

import argparse
from pathlib import Path

import chronicle


def build() -> chronicle.Workflow:
    """Make one "greet" task for each region, then a "done" task after all of them."""
    w = chronicle.Workflow("fanout-")
    greeting = w.param("greeting", "hello")
    greet = chronicle.container("greet", "busybox:1.37", "echo")
    greet.args(greet.input("text"))
    main = chronicle.dag("main")
    each = main.task("greet", greet).arg("text", greeting + " from {{item}}").with_items("eu", "us", "asia")
    main.task("done", greet).after(each).arg("text", "all regions done")
    return w.entrypoint(main)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--server", help="the URL of the chronicle server; without it, print the JSON")
    parser.add_argument("--namespace", default="default")
    parser.add_argument("--token-file", type=Path)
    args = parser.parse_args()
    w = build()
    if not args.server:
        print(w.to_json())
        return
    client = chronicle.Client(args.server, args.token_file.read_text().strip())
    print(f"Started {args.namespace}/{client.submit(args.namespace, w)}")


if __name__ == "__main__":
    main()
