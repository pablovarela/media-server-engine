# shellcheck shell=bash

readonly DIALOG_WIDTH=76
readonly KEYS_HINT="Arrows move, Enter chooses, Tab reaches the buttons, Esc goes back."
readonly EDIT_DONE=0 EDIT_BACK=1 EDIT_STOP=2
readonly WHIPTAIL_ESCAPE=255
readonly TIME_ZONE_REGIONS="Africa America Antarctica Arctic Asia Atlantic Australia Europe Indian Pacific"

dialog_value() {
  whiptail --title "Configure $INSTALLATION_NAME" "$@" 3>&1 1>&2 2>&3
}

edit_status() {
  if [ "$1" -eq "$WHIPTAIL_ESCAPE" ]; then echo "$EDIT_STOP"; else echo "$EDIT_BACK"; fi
}

field_display_value() {
  local variable=$1 kind=$2
  case $kind in
    secret | password) mask "${!variable:-}" ;;
    *) printf '%s' "${!variable:-}" ;;
  esac
}

fill_defaults() {
  local section variable kind label
  while IFS='|' read -r section variable kind label <&3; do
    case $kind in
      text | choice | timezone) [ -n "${!variable:-}" ] || printf -v "$variable" '%s' "$(field_default "$variable")" ;;
      *) keep_unasked_field "$variable" ;;
    esac
  done 3<<< "$FIELDS"
}

empty_answer_hint() {
  if [ -n "${!1:-}" ]; then
    echo "Leave empty to keep the current one."
  elif [ "$2" = password ]; then
    echo "Leave empty to generate one."
  else
    echo "Leave empty to leave it unset."
  fi
}

field_in_section() {
  [ "$1" = "$2" ] && field_applies "$3"
}

secret_value_or_current() {
  local variable=$1 kind=$2 typed=$3
  if [ -n "$typed" ]; then
    printf '%s' "$typed"
  elif [ -n "${!variable:-}" ]; then
    printf '%s' "${!variable}"
  elif [ "$kind" = password ]; then
    generate_secret 24
  fi
}

time_zones_in() {
  python3 -c '
import sys, zoneinfo
region = sys.argv[1] + "/"
for zone in sorted(zoneinfo.available_timezones()):
    if zone.startswith(region):
        print(zone[len(region):])' "$1"
}

pick_time_zone() {
  local variable=$1 text=$2 region city items status current=${!1:-}
  while true; do
    items=(UTC "Coordinated Universal Time")
    for region in $TIME_ZONE_REGIONS; do items+=("$region" ""); done
    status=0
    region=$(dialog_value --cancel-button Back --default-item "${current%%/*}" --menu "$text"$'\n\n'"Choose a region. $KEYS_HINT" 22 "$DIALOG_WIDTH" 12 "${items[@]}") || status=$?
    [ "$status" -eq 0 ] || return "$(edit_status "$status")"
    if [ "$region" = UTC ]; then
      printf -v "$variable" '%s' Etc/UTC
      return "$EDIT_DONE"
    fi
    items=()
    while IFS= read -r city; do items+=("$city" ""); done < <(time_zones_in "$region")
    status=0
    city=$(dialog_value --cancel-button Back --default-item "${current#*/}" --menu "Choose a place in $region. $KEYS_HINT" 22 "$DIALOG_WIDTH" 14 "${items[@]}") || status=$?
    [ "$status" -ne "$WHIPTAIL_ESCAPE" ] || return "$EDIT_STOP"
    if [ "$status" -eq 0 ]; then
      printf -v "$variable" '%s' "$region/$city"
      return "$EDIT_DONE"
    fi
  done
}

pick_choice() {
  local variable=$1 text=$2 items=() value description choice status=0
  while IFS='|' read -r value description; do items+=("$value" "$description"); done < <(field_choices "$variable")
  choice=$(dialog_value --cancel-button Back --default-item "${!variable:-}" --menu "$text"$'\n\n'"$KEYS_HINT" 16 "$DIALOG_WIDTH" 4 "${items[@]}") || status=$?
  [ "$status" -eq 0 ] || return "$(edit_status "$status")"
  printf -v "$variable" '%s' "$choice"
}

edit_field() {
  local variable=$1 kind=$2 label=$3 help text value status problem
  help=$(field_help "$variable")
  text=$label
  [ -z "$help" ] || text="$label"$'\n\n'"$help"
  while true; do
    status=0
    case $kind in
      timezone)
        pick_time_zone "$variable" "$text" || return $?
        return "$EDIT_DONE"
        ;;
      choice)
        pick_choice "$variable" "$text" || return $?
        return "$EDIT_DONE"
        ;;
      text)
        value=$(dialog_value --cancel-button Back --inputbox "$text" 14 "$DIALOG_WIDTH" "${!variable:-}") || status=$?
        ;;
      secret | password)
        value=$(dialog_value --cancel-button Back --passwordbox "$text"$'\n\n'"$(empty_answer_hint "$variable" "$kind")" 14 "$DIALOG_WIDTH") || status=$?
        [ "$status" -ne 0 ] || value=$(secret_value_or_current "$variable" "$kind" "$value")
        ;;
    esac
    [ "$status" -eq 0 ] || return "$(edit_status "$status")"
    value=$(field_normalize "$variable" "$value")
    problem=$(field_problem "$variable" "$value")
    if [ -n "$problem" ]; then
      dialog_value --msgbox "$problem" 8 "$DIALOG_WIDTH" || true
      continue
    fi
    printf -v "$variable" '%s' "$value"
    return "$EDIT_DONE"
  done
}

confirm_stop() {
  dialog_value --defaultno --yesno "Stop configuring $INSTALLATION_NAME? The answers so far are not saved." 10 "$DIALOG_WIDTH"
}

backup_kept() {
  local problem
  join_restic_repository
  problem=$(backup_problem)
  [ -n "$problem" ] || return 0
  dialog_value --defaultno --yesno "$problem"$'\n\n'"Keep it anyway?" 12 "$DIALOG_WIDTH"
}

walk_every_field() {
  local lines=() line section variable kind label next_section index=0 previous status
  while IFS= read -r line; do lines+=("$line"); done <<< "$FIELDS"
  while [ "$index" -lt "${#lines[@]}" ]; do
    IFS='|' read -r section variable kind label <<< "${lines[$index]}"
    if ! field_applies "$variable"; then
      index=$((index + 1))
      continue
    fi
    status=0
    edit_field "$variable" "$kind" "$label" || status=$?
    case $status in
      "$EDIT_DONE")
        next_section=""
        [ $((index + 1)) -ge "${#lines[@]}" ] || next_section=$(cut -d'|' -f1 <<< "${lines[$((index + 1))]}")
        if [ "$section" = Backup ] && [ "$next_section" != Backup ] && ! backup_kept; then
          while [ "$(cut -d'|' -f1 <<< "${lines[$((index - 1))]}")" = Backup ]; do index=$((index - 1)); done
          continue
        fi
        index=$((index + 1))
        ;;
      "$EDIT_BACK")
        previous=$((index - 1))
        while [ "$previous" -ge 0 ] && ! field_applies "$(cut -d'|' -f2 <<< "${lines[$previous]}")"; do previous=$((previous - 1)); done
        if [ "$previous" -ge 0 ]; then
          index=$previous
        elif confirm_stop; then
          return 1
        fi
        ;;
      *) if confirm_stop; then return 1; fi ;;
    esac
  done
}

section_status() {
  local wanted=$1 section variable kind label missing=0
  while IFS='|' read -r section variable kind label <&3; do
    field_in_section "$section" "$wanted" "$variable" || continue
    [ -n "${!variable:-}" ] || missing=$((missing + 1))
  done 3<<< "$FIELDS"
  if [ "$missing" -eq 0 ]; then echo "all set"; else echo "$missing not set"; fi
}

section_menu() {
  local wanted=$1 section variable kind label choice items
  while true; do
    items=()
    while IFS='|' read -r section variable kind label <&3; do
      field_in_section "$section" "$wanted" "$variable" || continue
      items+=("$variable" "${label%% (*}: $(field_display_value "$variable" "$kind")")
    done 3<<< "$FIELDS"
    items+=(Back "return to the sections")
    choice=$(dialog_value --cancel-button Back --menu "$wanted"$'\n\n'"$KEYS_HINT" 22 "$DIALOG_WIDTH" 12 "${items[@]}") || choice=Back
    if [ "$choice" = Back ]; then
      [ "$wanted" != Backup ] || backup_kept || continue
      return 0
    fi
    while IFS='|' read -r section variable kind label <&3; do
      [ "$variable" = "$choice" ] || continue
      edit_field "$variable" "$kind" "$label" || true
    done 3<<< "$FIELDS"
  done
}

main_menu() {
  local choice items section
  while true; do
    items=()
    while IFS= read -r section <&3; do
      items+=("$section" "$(section_status "$section")")
    done 3< <(sections)
    items+=(Save "save, commit and exit" Discard "leave without saving")
    choice=$(dialog_value --cancel-button Discard --menu "Choose a section to review or change."$'\n\n'"$KEYS_HINT" 22 "$DIALOG_WIDTH" 12 "${items[@]}") || choice=Discard
    case $choice in
      Save) return 0 ;;
      Discard) return 1 ;;
      *) section_menu "$choice" ;;
    esac
  done
}

menu_for_values() {
  fill_defaults
  if [ -n "${NEW_INSTALLATION:-}" ]; then
    walk_every_field || die "stopped before $INSTALLATION_NAME's settings were saved"
    until main_menu; do
      ! confirm_stop || die "stopped before $INSTALLATION_NAME's settings were saved"
    done
    return 0
  fi
  if ! main_menu; then
    echo "Nothing changed."
    exit 0
  fi
}
