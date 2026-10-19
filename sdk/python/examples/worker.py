"""Run the task "python-greet" of the e2e workflow test/e2e/testdata/tasks.yaml.

uv run examples/worker.py --server https://chronicle.example.com --namespace team-a --token-file token
"""

import argparse
import signal
import threading
from pathlib import Path

import chronicle


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--server", required=True)
    parser.add_argument("--namespace", default="default")
    parser.add_argument("--token-file", type=Path, required=True)
    args = parser.parse_args()
    client = chronicle.Client(args.server, args.token_file.read_text().strip())
    worker = chronicle.Worker(client, args.namespace, concurrency=2)

    @worker.task("python-greet")
    def greet(data: dict[str, str], task: chronicle.TaskInfo) -> dict[str, str]:
        return {"greeting": f"Python says hello to {data['who']}"}

    # SIGTERM and Ctrl-C stop the polling; running tasks get the grace period.
    stop = threading.Event()
    signal.signal(signal.SIGTERM, lambda *_: stop.set())
    signal.signal(signal.SIGINT, lambda *_: stop.set())
    worker.run(stop)


if __name__ == "__main__":
    main()
