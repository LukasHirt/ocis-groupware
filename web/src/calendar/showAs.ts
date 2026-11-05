type Gettext = (message: string) => string

// The choices of how an event shows in a free and busy view, in the order the
// editor offers them.
export function showAsOptions($gettext: Gettext): { value: string; label: string }[] {
  return [
    { value: 'free', label: $gettext('Free') },
    { value: 'tentative', label: $gettext('Tentative') },
    { value: 'busy', label: $gettext('Busy') },
    { value: 'oof', label: $gettext('Away') },
    { value: 'workingElsewhere', label: $gettext('Working elsewhere') },
    { value: 'unknown', label: $gettext('Unknown') }
  ]
}

export function showAsLabel(value: string, $gettext: Gettext): string {
  return showAsOptions($gettext).find((o) => o.value === value)?.label ?? value
}
