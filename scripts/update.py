#!/usr/bin/env python3
import sys

from engine import installation, update

sys.stdout.reconfigure(line_buffering=True)
installation.export_directories()
sys.exit(update.main(sys.argv[1:]))
