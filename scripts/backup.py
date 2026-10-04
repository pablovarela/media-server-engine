#!/usr/bin/env python3
import sys

from engine import backups, program

program.start()
sys.exit(backups.main(sys.argv[1:]))
