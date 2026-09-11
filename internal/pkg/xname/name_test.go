package xname

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalize(t *testing.T) {
	for _, name := range []string{"张三", "小明", "John  Smith", "王 小明", strings.Repeat("张", 100), "🦊 小助手"} {
		t.Run(name, func(t *testing.T) {
			got, err := Normalize(" \t" + name + "\n")
			require.NoError(t, err)
			require.Equal(t, name, got)
		})
	}
	for _, name := range []string{"", " \t\n\u3000", strings.Repeat("张", 101), string([]byte{0xff})} {
		_, err := Normalize(name)
		require.Error(t, err)
	}
}

func TestFromLegacy(t *testing.T) {
	for _, tc := range []struct{ first, last, want string }{
		{" 三 ", " 张 ", "张三"},
		{"John", "Smith", "John Smith"},
		{"小明", "", "小明"},
		{"", "张", "张"},
		{" ", "", ""},
		{"小太郎", "山田", "山田小太郎"},
	} {
		require.Equal(t, tc.want, FromLegacy(tc.first, tc.last))
	}
}
