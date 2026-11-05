#!/usr/bin/env python3
"""Opt-in experimental Vulkan remoting for a headless local application.

This does not install a system ICD or provide a game/window presentation path.
"""
import argparse
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import sys


def stop(process):
    if process is None or process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        process.wait()
        return
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--node', default='STUDIO')
    parser.add_argument('--expect', default='')
    parser.add_argument('--trace', action='store_true')
    parser.add_argument('--timeout', type=float, default=None,
                        help='optional application runtime limit in seconds')
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.timeout is not None and args.timeout <= 0:
        parser.error('--timeout must be positive')
    command = args.command[1:] if args.command[:1] == ['--'] else args.command
    if not command:
        parser.error('provide a headless application after --')
    root = Path(__file__).resolve().parents[2]
    binary = root/'dist/omls-render'
    icd = root/'sharing/development/mesa-build/src/gfxstream/guest/vulkan/gfxstream_vk_devenv_icd.x86_64.json'
    if not binary.is_file() or not icd.is_file():
        parser.error('build the experimental OMLS command and Mesa guest first')
    session_command = [str(binary), 'render', 'session', '--node', args.node,
                       '--owner-pid', str(os.getpid())]
    if args.expect:
        session_command += ['--expect', args.expect]
    if args.trace:
        session_command += ['--trace']
    session = subprocess.Popen(session_command, stdout=subprocess.PIPE,
                               text=True, start_new_session=True)
    application = None
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(session.stdout, selectors.EVENT_READ)
            if not selector.select(60):
                raise RuntimeError('remote GPU session startup timed out')
        ready = json.loads(session.stdout.readline())
        env = os.environ.copy()
        env.update(OMLS_RENDER_SOCKET=ready['socket'], VK_DRIVER_FILES=str(icd),
                   VK_ICD_FILENAMES=str(icd), VK_LOADER_LAYERS_DISABLE='~implicit~')
        env.pop('LD_PRELOAD', None)
        application = subprocess.Popen(command, env=env, start_new_session=True)
        return application.wait(timeout=args.timeout)
    finally:
        stop(application)
        stop(session)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        sys.exit(130)
