// Package configured is a complete program whose settings are described,
// read from several places, and provided to several consumers.
//
// The shape is the one a real service has. Two components -- a store and a
// mailer -- each describe what they need without knowing what the other needs
// or where any of it comes from. The deployment supplies some of it in the
// environment, the rest comes from the defaults the program shipped with, and a
// plugin is configured from a document of its own. Nothing is passed down: a
// component reads its settings through the runtime, so adding a setting does
// not change a signature anywhere above it.
package configured

import (
	"time"

	"github.com/mbauer83/effect-golang/effect/config"
)

// Store is what the storage component needs to be told.
type Store struct {
	Host     string
	Port     int
	Password config.Secret
	Timeout  time.Duration
	// Limits are read from whatever keys the deployment happens to hold, so
	// adding a limit is a deployment change and not a release.
	Limits map[string]int
}

// Mailer is what the delivery component needs to be told.
//
// Its Sender has no default: a service that sends mail from an address nobody
// chose is worse than one that will not start.
type Mailer struct {
	Sender  string
	Relays  []string
	Retries int
}

// DescribedStore is the storage component's own description of its settings.
//
// One field per line: where the value comes from, and where it goes. Every
// field is read whatever the ones before it did, so a deployment that has
// supplied none of them is told about all of them.
//
// Read beneath "db", so the same description serves a primary and a replica,
// and so the component's names cannot collide with another component's.
func DescribedStore() config.Config[Store] {
	return config.Nested("db", config.Struct(
		config.Setting(
			config.NonEmptyText("host").Documented("the address of the primary"),
			func(store *Store, host string) { store.Host = host }),
		config.Setting(
			config.Port("port").WithDefault(5432),
			func(store *Store, port int) { store.Port = port }),
		config.Setting(
			config.SecretOf("password").
				Documented("supplied by the deployment, never logged"),
			func(store *Store, password config.Secret) { store.Password = password }),
		config.Setting(
			config.Duration("timeout").
				WithDefault(5*time.Second).
				Documented("how long a statement may take"),
			func(store *Store, timeout time.Duration) { store.Timeout = timeout }),
		config.Setting(
			config.Table("limits", config.Int("")).
				Documented("one limit per operation the deployment cares about"),
			func(store *Store, limits map[string]int) { store.Limits = limits }),
	))
}

// DescribedMailer is the delivery component's description.
//
// Its relays are several values held in one key, which is how a flat source
// spells a list, and each of them is read by the description that knows what a
// relay is -- so a blank one is refused where it is written rather than when
// something tries to connect to it.
func DescribedMailer() config.Config[Mailer] {
	return config.Nested("mail", config.Struct(
		config.Setting(
			config.NonEmptyText("sender").Documented("the address mail is sent from"),
			func(mailer *Mailer, sender string) { mailer.Sender = sender }),
		config.Setting(
			config.Many("relays", ",", config.NonEmptyText("")).
				Documented("the relays to try, in order"),
			func(mailer *Mailer, relays []string) { mailer.Relays = relays }),
		config.Setting(
			config.Int("retries").
				WithDefault(3).
				Validated("at most ten retries", func(retries int) bool {
					return retries >= 0 && retries <= 10
				}),
			func(mailer *Mailer, retries int) { mailer.Retries = retries }),
	))
}

// Reporting is a plugin's settings, and it is deliberately small: what it
// shows is that a part of a program can be configured from somewhere the rest
// of the program does not read.
type Reporting struct {
	Every   time.Duration
	Enabled bool
}

// DescribedReporting reads a schedule, and folds absence into the value rather
// than into an option nobody can forget to check.
func DescribedReporting() config.Config[Reporting] {
	return config.Optional(
		config.Duration("every").Documented("how often to report, if at all"),
		func(every time.Duration) Reporting {
			return Reporting{Every: every, Enabled: true}
		},
		func() Reporting {
			return Reporting{}
		})
}

// Defaults are what the program ships with: the source of last resort, beneath
// whatever the deployment supplies.
//
// A source rather than more defaults on the description, because these are one
// deployment's opinions rather than the component's -- and because a chart, a
// compose file or a test can replace the whole set at once.
func Defaults() config.Source {
	return config.Fixed(map[string]string{
		"db.host":     "localhost",
		"db.password": "development-only",
		"mail.sender": "nobody@example.invalid",
		"mail.relays": "localhost:25",
	})
}
