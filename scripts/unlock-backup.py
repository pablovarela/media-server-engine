#!/usr/bin/env python3
import sys

from engine import program, unlock

program.start()
sys.exit(unlock.main(sys.argv[1:]))
