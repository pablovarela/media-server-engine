#!/usr/bin/env python3
import sys

from engine import program, verify

program.start()
sys.exit(verify.main(sys.argv[1:]))
