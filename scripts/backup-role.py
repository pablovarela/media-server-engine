#!/usr/bin/env python3
import sys

from engine import program, role

program.start()
sys.exit(role.main(sys.argv[1:]))
