#!/usr/bin/env python3
import sys

from engine import claim, program

program.start()
sys.exit(claim.main(sys.argv[1:]))
