package cmd

import "os/user"

func currentAccount() (string, string, error) {
	current, err := user.Current()
	if err != nil {
		return "", "", err
	}
	group, err := user.LookupGroupId(current.Gid)
	if err != nil {
		return "", "", err
	}
	return current.Username, group.Name, nil
}
