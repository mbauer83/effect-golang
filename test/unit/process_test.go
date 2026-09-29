package unit

// A program's main: run until done or stopped, then close the runtime.

import (
	"errors"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effect/config"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestAProgramThatEndsOrIsStoppedIsNoFailure(t *testing.T) {
	for name, program := range map[string]effect.Effect[effect.Unit, error, effect.Unit]{
		"ended":   effect.Succeed[effect.Unit, error](effect.Unit{}),
		"stopped": effecttest.InterruptSelf[effect.Unit, error, effect.Unit](),
	} {
		runtime, err := effect.NewRuntime()
		if err != nil {
			t.Fatal(err)
		}
		if err := effect.RunUntilStopped(runtime, program); err != nil {
			t.Errorf("%s: expected no failure, got %v", name, err)
		}
	}
}

func TestAProgramThatFailedSaysWhy(t *testing.T) {
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	err = effect.RunUntilStopped(runtime, effect.Fail[effect.Unit, effect.Unit](errors.New("the port is taken")))
	if err == nil || !strings.Contains(err.Error(), "the port is taken") {
		t.Errorf("expected the cause, got %v", err)
	}
}

func TestLoadingTellsADeploymentEverythingItReads(t *testing.T) {
	type settings struct{ Host, Region string }
	description := config.Struct(
		config.Setting(config.NonEmptyText("host").WithDescription("where to connect"), func(s *settings, v string) { s.Host = v }),
		config.Setting(config.Text("region").WithDefault("eu"), func(s *settings, v string) { s.Region = v }),
	)
	_, err := config.Load(config.FromMap(map[string]string{}), description)
	if err == nil || !strings.Contains(err.Error(), "host") || !strings.Contains(err.Error(), "where to connect") ||
		!strings.Contains(err.Error(), "region") {
		t.Errorf("expected what is missing and the whole list, got %v", err)
	}
	loaded, err := config.Load(config.FromMap(map[string]string{"host": "db"}), description)
	if err != nil || loaded != (settings{Host: "db", Region: "eu"}) {
		t.Errorf("expected the settings, got %+v, %v", loaded, err)
	}
}
