#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later
"""Adapter: OpenClaw CLI → PANDA Commander.

Protocol (shared with codex.py / claude_code.py / opencode.py):
  stdin:  a JSON object with keys {prompt, timeout_s, cwd} (cwd optional),
          plus optional {resume, tools_policy}
  stdout: a JSON object with keys {ok, result, exit_code, tokens, cost}

Runs `openclaw agent exec <prompt>` headlessly. stdout is returned verbatim
and a non-zero exit becomes the diagnosis. openclaw exposes no documented
session, usage or tool-whitelist surface, so resume/tools_policy ride the
request but do not change the command line, and tokens/cost stay unset.
This adapter never prints secrets.

The wire contract, watchdog timeout, process-tree cleanup and stderr
diagnostics live in _harness.py; this file is only the openclaw difference:
the command line and the heartbeat cadence of its silent run.
"""
import _harness as harness

# How often a silent run says it is still alive. `openclaw agent exec` prints
# nothing until the run finishes, so without this the Go silence watchdog sees
# a dead pipe for the whole run and kills a healthy long task at the default
# 4-minute silence limit. The heartbeat does not shorten a wedged run: the
# hard agent budget still bounds it.
HEARTBEAT_S = 20


def main():
    prompt, timeout, cwd = harness.read_request()
    harness.run_simple(["openclaw", "agent", "exec", prompt],
                       cwd=cwd, timeout=timeout, heartbeat_s=HEARTBEAT_S)


if __name__ == "__main__":
    main()
