package parity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	c1 := &stubComparator{name: "alpha"}
	c2 := &stubComparator{name: "beta"}

	reg.Register(c1)
	reg.Register(c2)

	got, ok := reg.Get("alpha")
	require.True(t, ok)
	require.Equal(t, "alpha", got.Name())

	got, ok = reg.Get("beta")
	require.True(t, ok)
	require.Equal(t, "beta", got.Name())
}

func TestRegistry_GetMissing(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.Get("nonexistent")
	require.False(t, ok)
}

func TestRegistry_NamesSorted(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubComparator{name: "zulu"})
	reg.Register(&stubComparator{name: "alpha"})
	reg.Register(&stubComparator{name: "mike"})

	names := reg.Names()
	require.Equal(t, []string{"alpha", "mike", "zulu"}, names)
}

func TestRegistry_All(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubComparator{name: "b"})
	reg.Register(&stubComparator{name: "a"})

	all := reg.All()
	require.Len(t, all, 2)
	require.Equal(t, "a", all[0].Name())
	require.Equal(t, "b", all[1].Name())
}

func TestRegistry_Empty(t *testing.T) {
	reg := NewRegistry()
	require.Empty(t, reg.Names())
	require.Empty(t, reg.All())
}
