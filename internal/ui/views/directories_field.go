package views

// dirField is the people directory's profile field filter: Slack's
// browser filter "profileField.<id>" that a profile's field link sets
// (boot-shared kDid RimetoProfileFieldLink → WyBE navigateToBrowserPage
// {entity: PEOPLE, initialFilter}), sent as search.modules people's
// custom_fields {"<id>": [value]} (client-boot's browser request). Reset
// clears it.
type dirField struct{ id, value string }

// custom is the filter as core.PeopleBrowseOpts.CustomFields (nil: none).
func (f dirField) custom() map[string][]string {
	if f.id == "" || f.value == "" {
		return nil
	}
	return map[string][]string{f.id: {f.value}}
}

// clear drops the filter and reports whether there was one.
func (f *dirField) clear() bool {
	had := f.id != ""
	*f = dirField{}
	return had
}

// FilterByProfileField opens the People tab filtered to the members whose
// profile field id has value (route directories?tab=people&field=&value=).
func (d *Directories) FilterByProfileField(id, value string) {
	d.field = dirField{id: id, value: value}
	d.ShowTab(dirTabPeople)
}

// FilterByAccountType opens the People tab with its account type filter
// on key ("admins": route directories?tab=people&account=admins), what
// Slack's openPeopleBrowser({filters: {accountType}}) does for a user
// group pane's "workspace administrator" link.
func (d *Directories) FilterByAccountType(key string) {
	for i, k := range dirAcctKeys {
		if k == key && k != "" {
			d.acct.sel = i
		}
	}
	d.ShowTab(dirTabPeople)
}
