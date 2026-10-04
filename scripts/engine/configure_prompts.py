import os
import sys

from engine import commands, prompt, settings


def input_ended():
    return bool(os.environ.get("PROMPT_INPUT_ENDED"))


def answer(field, label, values):
    if field.kind == "secret":
        return prompt.ask_secret(label, values.get(field.name, ""))
    if field.kind == "password":
        return prompt.ask_password(label, values.get(field.name, ""))
    return prompt.ask(label, settings.default(field.name, values))


def forget_unusable_saved_value(field, values):
    if settings.problem(field.name, values.get(field.name, "")):
        values[field.name] = ""


def ask_field(field, values):
    help_text = settings.help_text(field.name, values)
    if help_text:
        print(f"  {help_text}", file=sys.stderr)
    label = field.label
    if field.kind == "choice":
        choices = settings.CHOICES[field.name]
        for choice, description in choices:
            print(f"    {choice}: {description}", file=sys.stderr)
        label = f"{label} ({'/'.join(choice for choice, _ in choices)})"
    while True:
        value = settings.normalize(field.name, answer(field, label, values))
        problem = settings.problem(field.name, value)
        if not problem:
            values[field.name] = value
            return
        print(problem, file=sys.stderr)
        if input_ended():
            raise commands.Stop(f"the answers ran out while {label} was not valid")
        forget_unusable_saved_value(field, values)


def ask_section(section, values):
    for field in settings.FIELDS:
        if field.section != section:
            continue
        if settings.applies(field.name, values):
            ask_field(field, values)
        else:
            values.setdefault(field.name, "")


def backup_rejected(values):
    settings.join_repository(values)
    problem = settings.backup_problem(values)
    if not problem:
        return False
    print(problem, file=sys.stderr)
    keep = prompt.ask("Keep it anyway? (y/n)", "n")
    if input_ended():
        raise commands.Stop("the answers ran out before the backup details were settled")
    return keep not in ("y", "Y")


def fill(values):
    print(f"Installation: {values['INSTALLATION_NAME']}", file=sys.stderr)
    for section in settings.sections(values):
        if section != "Installation":
            print(section, file=sys.stderr)
        ask_section(section, values)
        while section == "Backup" and backup_rejected(values):
            ask_section(section, values)
