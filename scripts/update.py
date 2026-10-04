#!/usr/bin/env python3
import sys

from engine import program, update

program.start()
sys.exit(update.main(sys.argv[1:]))
