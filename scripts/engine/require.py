from engine import installation, program


def require_installation(argv):
    installation.require()


def main(argv):
    return program.run("require-installation", require_installation, argv)
