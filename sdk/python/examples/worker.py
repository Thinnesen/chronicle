"""Run the task "python-greet" of the e2e workflow test/e2e/testdata/tasks.yaml.

uv run examples/worker.py --server https://chronicle.example.com --namespace team-a --token-file token
"""

import argparse
import faulthandler
import signal
from pathlib import Path

import chronicle


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--server", required=True)
    parser.add_argument("--namespace", default="default")
    parser.add_argument("--token-file", type=Path, required=True)
    args = parser.parse_args()
    # The client reads the file for each call, so a projected token that Kubernetes rotates keeps working.
    client = chronicle.Client.from_token_file(args.server, args.token_file)
    worker = chronicle.Worker(client, args.namespace, concurrency=2)

    @worker.task("python-greet")
    def greet(data: dict[str, str], task: chronicle.TaskInfo) -> dict[str, str]:
        return {"greeting": f"Python says hello to {data['who']}"}

    # SIGTERM and Ctrl-C stop the polling; running tasks get the grace period.
    stop = chronicle.stop_on_signals()
    # SIGUSR1 prints the stack of each thread, to find out where a worker waits.
    faulthandler.register(signal.SIGUSR1, all_threads=True)
    worker.run(stop)


if __name__ == "__main__":
    main()
