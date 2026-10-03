#!/usr/bin/env python3
import sys

from engine import installation

sys.stdout.reconfigure(line_buffering=True)

installation.export_directories()

# The engine modules read the directories when they are imported.
from engine import update  # noqa: E402

sys.exit(update.main(sys.argv[1:]))
