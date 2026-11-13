package application

import (
	"html/template"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestList(t *testing.T) {
	list := templateFuncs()["list"].(func(...interface{}) []interface{})

	t.Run("keeps its items in the order given", func(t *testing.T) {
		assert.Equal(t, []interface{}{"weekly", 2, true}, list("weekly", 2, true))
	})

	t.Run("is empty given nothing, so ranging over it draws nothing", func(t *testing.T) {
		assert.Empty(t, list())
	})

	t.Run("lets a template range over items written in place", func(t *testing.T) {
		tpl := template.Must(template.New("").Funcs(templateFuncs()).Parse(
			`{{ range list (dict "Key" "a" "Label" "A") (dict "Key" "b" "Label" "B") }}{{ .Key }}={{ .Label }};{{ end }}`))
		var out strings.Builder
		require.NoError(t, tpl.Execute(&out, nil))
		assert.Equal(t, "a=A;b=B;", out.String())
	})
}
