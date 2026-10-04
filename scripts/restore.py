#!/usr/bin/env python3
import sys

from engine import program, restore

program.start()
sys.exit(restore.main(sys.argv[1:]))
