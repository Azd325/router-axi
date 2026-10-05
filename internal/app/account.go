package app

import (
	"fmt"
	"io"
	"strconv"

	"github.com/Azd325/router-axi/internal/tr064"
)

func writeAccount(w io.Writer, value tr064.Account) error {
	if _, err := fmt.Fprintf(w, "account:\n  username: %s\n  anonymous_login_enabled: %t\n  default_password_active: %s\n  second_factor_enabled: %t\n", strconv.Quote(value.Username), value.AnonymousLoginEnabled, optionalBool(value.DefaultPasswordActive), value.SecondFactorEnabled); err != nil {
		return err
	}
	if value.Rights == nil {
		_, err := fmt.Fprintln(w, "  rights: unknown")
		return err
	}
	if len(value.Rights) == 0 {
		_, err := fmt.Fprintln(w, "  rights: 0 configured rights reported")
		return err
	}
	if _, err := fmt.Fprintf(w, "  rights[%d]{path,access}:\n", len(value.Rights)); err != nil {
		return err
	}
	for _, right := range value.Rights {
		if _, err := fmt.Fprintf(w, "    %s,%s\n", toon(right.Path), toon(right.Access)); err != nil {
			return err
		}
	}
	return nil
}
