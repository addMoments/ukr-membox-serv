package utils

import "fmt"

func Tag_err(prefix string, err error) error {
	return fmt.Errorf(prefix + " | " + err.Error())
}

func Merge_err(errs []error) error {
	err_msgs := []string{}

	for i := 0; i < len(errs); i++ {
		if errs[i] != nil {
			emsg := errs[i].Error()
			if emsg == "" {
				emsg = "empty_error"
			}
			err_msgs = append(err_msgs, emsg)
		}
	}

	if len(err_msgs) == 0 {
		return nil
	}

	for i := 1; i < len(err_msgs); i++ {
		err_msgs[0] += " | " + err_msgs[i]
	}

	return fmt.Errorf(err_msgs[0])

}
