# shellcheck shell=bash

readonly DIALOG_WIDTH=76

dialog_value() {
  whiptail --title "Configure $INSTALLATION_NAME" "$@" 3>&1 1>&2 2>&3
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
      text | yesno) [ -n "${!variable:-}" ] || printf -v "$variable" '%s' "$(field_default "$variable")" ;;
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

edit_field() {
  local variable=$1 kind=$2 label=$3 help text value status problem
  help=$(field_help "$variable")
  text=$label
  [ -z "$help" ] || text="$label"$'\n\n'"$help"
  while true; do
    status=0
    case $kind in
      text)
        value=$(dialog_value --inputbox "$text" 12 "$DIALOG_WIDTH" "${!variable:-}") || status=$?
        ;;
      yesno)
        local default_no=()
        [ "${!variable:-}" = y ] || default_no=(--defaultno)
        dialog_value ${default_no[@]+"${default_no[@]}"} --yesno "$text" 10 "$DIALOG_WIDTH" || status=$?
        case $status in
          0) value=y ;;
          1) value=n status=0 ;;
        esac
        ;;
      secret | password)
        local hint
        hint=$(empty_answer_hint "$variable" "$kind")
        value=$(dialog_value --passwordbox "$text"$'\n\n'"$hint" 12 "$DIALOG_WIDTH") || status=$?
        [ "$status" -ne 0 ] || value=$(secret_value_or_current "$variable" "$kind" "$value")
        ;;
    esac
    [ "$status" -eq 0 ] || return 1
    problem=$(field_problem "$variable" "$value")
    if [ -n "$problem" ]; then
      dialog_value --msgbox "$problem" 8 "$DIALOG_WIDTH" || true
      continue
    fi
    printf -v "$variable" '%s' "$value"
    return 0
  done
}

walk_every_field() {
  local section variable kind label
  while IFS='|' read -r section variable kind label <&3; do
    field_applies "$variable" || continue
    edit_field "$variable" "$kind" "$label" || return 1
  done 3<<< "$FIELDS"
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
    choice=$(dialog_value --cancel-button Back --menu "$wanted" 20 "$DIALOG_WIDTH" 12 "${items[@]}") || return 0
    while IFS='|' read -r section variable kind label <&3; do
      [ "$variable" = "$choice" ] || continue
      edit_field "$variable" "$kind" "$label" || true
    done 3<<< "$FIELDS"
  done
}

sections() {
  cut -d'|' -f1 <<< "$FIELDS" | uniq
}

main_menu() {
  local choice items section
  while true; do
    items=()
    while IFS= read -r section <&3; do
      items+=("$section" "$(section_status "$section")")
    done 3< <(sections)
    items+=(Save "save, commit and exit")
    choice=$(dialog_value --cancel-button Discard --menu "Choose a section to review or change." 20 "$DIALOG_WIDTH" 12 "${items[@]}") || return 1
    [ "$choice" != Save ] || return 0
    section_menu "$choice"
  done
}

menu_for_values() {
  fill_defaults
  if [ -n "${NEW_INSTALLATION:-}" ]; then
    walk_every_field || die "stopped before $INSTALLATION_NAME's settings were saved"
    main_menu || die "stopped before $INSTALLATION_NAME's settings were saved"
    return 0
  fi
  if ! main_menu; then
    echo "Nothing changed."
    exit 0
  fi
}
