package main

import (
	"testing"

	"github.com/Azd325/router-axi/internal/nettest"
)

func TestMain(m *testing.M) { nettest.Main(m) }

func TestOrdinaryTestsCannotReachNonLoopbackAddresses(t *testing.T) { nettest.AssertGuarded(t) }
