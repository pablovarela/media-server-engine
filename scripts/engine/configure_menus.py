import enum
import zoneinfo

from engine import commands, prompt, settings

DIALOG_WIDTH = "76"
KEYS_HINT = "Arrows move, Enter chooses, Tab reaches the buttons, Esc goes back."
ESCAPE = 255
TIME_ZONE_REGIONS = ["Africa", "America", "Antarctica", "Arctic", "Asia", "Atlantic", "Australia", "Europe", "Indian", "Pacific"]


class StoppedBeforeSaving(commands.Stop):
    pass


class Edit(enum.Enum):
    DONE = "done"
    BACK = "back"
    STOP = "stop"


def dialog(values, *args):
    return commands.dialog(["whiptail", "--title", f"Configure {values['INSTALLATION_NAME']}", *args])


def message(values, text, height):
    dialog(values, "--msgbox", text, str(height), DIALOG_WIDTH)


def confirmed(values, text, height):
    code, _ = dialog(values, "--defaultno", "--yesno", text, str(height), DIALOG_WIDTH)
    return code == 0


def left_with(code):
    return Edit.STOP if code == ESCAPE else Edit.BACK


def menu_items(pairs):
    return [word for pair in pairs for word in pair]


def display_value(values, field):
    value = values.get(field.name, "")
    return prompt.mask(value) if field.kind in ("secret", "password") else value


def fill_defaults(values):
    for field in settings.FIELDS:
        if field.kind in ("text", "choice", "timezone"):
            if not values.get(field.name):
                values[field.name] = settings.default(field.name, values)
        else:
            values.setdefault(field.name, "")


def empty_answer_hint(values, field):
    if values.get(field.name):
        return "Leave empty to keep the current one."
    if field.kind == "password":
        return "Leave empty to generate one."
    return "Leave empty to leave it unset."


def typed_or_current(values, field, typed):
    if typed or values.get(field.name):
        return typed or values[field.name]
    return prompt.generated_password() if field.kind == "password" else ""


def places_in(region):
    prefix = f"{region}/"
    return [zone[len(prefix):] for zone in sorted(zoneinfo.available_timezones()) if zone.startswith(prefix)]


def pick_time_zone(values, name, text):
    current = values.get(name, "")
    regions = [("UTC", "Coordinated Universal Time"), *[(region, "") for region in TIME_ZONE_REGIONS]]
    while True:
        code, region = dialog(
            values, "--cancel-button", "Back", "--default-item", current.split("/")[0],
            "--menu", f"{text}\n\nChoose a region. {KEYS_HINT}", "22", DIALOG_WIDTH, "12", *menu_items(regions),
        )
        if code != 0:
            return left_with(code)
        if region == "UTC":
            values[name] = "Etc/UTC"
            return Edit.DONE
        code, place = dialog(
            values, "--cancel-button", "Back", "--default-item", current.split("/", 1)[-1],
            "--menu", f"Choose a place in {region}. {KEYS_HINT}", "22", DIALOG_WIDTH, "14", *menu_items((place, "") for place in places_in(region)),
        )
        if code == ESCAPE:
            return Edit.STOP
        if code == 0:
            values[name] = f"{region}/{place}"
            return Edit.DONE


def pick_choice(values, name, text):
    code, choice = dialog(
        values, "--cancel-button", "Back", "--default-item", values.get(name, ""),
        "--menu", f"{text}\n\n{KEYS_HINT}", "16", DIALOG_WIDTH, "4", *menu_items(settings.CHOICES[name]),
    )
    if code != 0:
        return left_with(code)
    values[name] = choice
    return Edit.DONE


def edit_field(values, field):
    help_text = settings.help_text(field.name, values)
    text = f"{field.label}\n\n{help_text}" if help_text else field.label
    if field.kind == "timezone":
        return pick_time_zone(values, field.name, text)
    if field.kind == "choice":
        return pick_choice(values, field.name, text)
    while True:
        if field.kind == "text":
            code, value = dialog(values, "--cancel-button", "Back", "--inputbox", text, "14", DIALOG_WIDTH, values.get(field.name, ""))
        else:
            code, value = dialog(values, "--cancel-button", "Back", "--passwordbox", f"{text}\n\n{empty_answer_hint(values, field)}", "14", DIALOG_WIDTH)
            value = typed_or_current(values, field, value) if code == 0 else value
        if code != 0:
            return left_with(code)
        value = settings.normalize(field.name, value)
        problem = settings.problem(field.name, value)
        if not problem:
            values[field.name] = value
            return Edit.DONE
        message(values, problem, 8)


def stop_confirmed(values):
    return confirmed(values, f"Stop configuring {values['INSTALLATION_NAME']}? The answers so far are not saved.", 10)


def backup_kept(values):
    settings.join_repository(values)
    problem = settings.backup_problem(values)
    return not problem or confirmed(values, f"{problem}\n\nKeep it anyway?", 12)


def leaves_backup(fields, index):
    return fields[index].section == "Backup" and (index + 1 == len(fields) or fields[index + 1].section != "Backup")


def walk_every_field(values):
    fields = settings.FIELDS
    index = 0
    while index < len(fields):
        field = fields[index]
        if not settings.applies(field.name, values):
            index += 1
            continue
        edited = edit_field(values, field)
        if edited is Edit.DONE:
            if leaves_backup(fields, index) and not backup_kept(values):
                index = next(position for position, other in enumerate(fields) if other.section == "Backup")
            else:
                index += 1
        elif edited is Edit.BACK:
            previous = index - 1
            while previous >= 0 and not settings.applies(fields[previous].name, values):
                previous -= 1
            if previous >= 0:
                index = previous
            elif stop_confirmed(values):
                return False
        elif stop_confirmed(values):
            return False
    return True


def section_status(values, section):
    missing = sum(1 for field in settings.fields_in(section, values) if not values.get(field.name))
    return "all set" if missing == 0 else f"{missing} not set"


def section_menu(values, section):
    while True:
        fields = settings.fields_in(section, values)
        entries = [(field.name, f"{field.label.split(' (')[0]}: {display_value(values, field)}") for field in fields]
        code, choice = dialog(
            values, "--cancel-button", "Back", "--menu", f"{section}\n\n{KEYS_HINT}", "22", DIALOG_WIDTH, "12",
            *menu_items(entries), "Back", "return to the sections",
        )
        if code != 0 or choice == "Back":
            if section != "Backup" or backup_kept(values):
                return
            continue
        edit_field(values, next(field for field in fields if field.name == choice))


def main_menu(values):
    while True:
        entries = [(section, section_status(values, section)) for section in settings.sections(values)]
        code, choice = dialog(
            values, "--cancel-button", "Discard", "--menu", f"Choose a section to review or change.\n\n{KEYS_HINT}", "22", DIALOG_WIDTH, "12",
            *menu_items(entries), "Save", "save, commit and exit", "Discard", "leave without saving",
        )
        if code != 0 or choice == "Discard":
            return False
        if choice == "Save":
            return True
        section_menu(values, choice)


def stopped(values):
    return StoppedBeforeSaving(f"stopped before {values['INSTALLATION_NAME']}'s settings were saved")


def fill(values, new):
    fill_defaults(values)
    if not new:
        return main_menu(values)
    if not walk_every_field(values):
        raise stopped(values)
    while not main_menu(values):
        if stop_confirmed(values):
            raise stopped(values)
    return True
