from engine import commands, installation, program, restic


def unlock(argv):
    installation.load_installation()
    option = argv[0] if argv else ""
    if option == "":
        restic.run("unlock")
    elif option == "--remove-all":
        restic.run("unlock", "--remove-all")
    else:
        raise commands.Stop(f"unknown option {option}; --remove-all removes every lock")
    remaining = restic.describe_locks()
    if not remaining:
        print("no locks left on the backup repository")
        return
    print("Locks left, held by restic processes that may still be running:")
    for line in remaining:
        print(line)
    print("If none of those machines is running restic now, remove them with: make unlock-backup ALL=1")


def main(argv):
    return program.run("unlock-backup", unlock, argv)
