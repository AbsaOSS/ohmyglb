/*
Copyright 2021 Absa Group Limited

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package log

import (
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/rs/zerolog"
)

// Adapter implements logr.Logger interface.
// The adapter allows us to encapsulate zerolog into go-logr/logr interface.
type Adapter struct {
	z             *zerolog.Logger
	keysAndValues map[string]string
	name          string
}

func NewAdapter(l *zerolog.Logger) *Adapter {
	kv := make(map[string]string)
	return &Adapter{
		l,
		kv,
		"",
	}
}

func (a *Adapter) Enabled() bool {
	return true
}

func (a *Adapter) Info(msg string, keysAndValues ...interface{}) {
	a.WithValues(keysAndValues)
	if a.name != "" {
		a.z.Info().Msgf("%s: %s %s", a.name, msg, a.valuesAsJSON())
	}
	a.z.Info().Msgf("%s %s", msg, a.valuesAsJSON())
}

func (a *Adapter) Error(err error, msg string, keysAndValues ...interface{}) {
	a.WithValues(keysAndValues)
	if a.name != "" {
		a.z.Err(err).Msgf("%s: %s %s", a.name, msg, a.valuesAsJSON())
		return
	}
	a.z.Err(err).Msgf("%s %s", msg, a.valuesAsJSON())
}

func (a *Adapter) V(level int) logr.Logger {
	if level <= 0 {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	if level == 1 {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	if level >= 2 {
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	}
	return a
}

func (a *Adapter) WithValues(keysAndValues ...interface{}) logr.Logger {
	for i := 0; i < len(keysAndValues)/2; i++ {
		keyIndex := i * 2
		valIndex := keyIndex + 1
		key := fmt.Sprintf("%s", keysAndValues[keyIndex])
		if i*2+1 > len(keysAndValues) {
			continue
		}
		value := fmt.Sprintf("%s", keysAndValues[valIndex])
		a.keysAndValues[key] = value
	}
	return a
}

func (a *Adapter) WithName(name string) logr.Logger {
	a.name = name
	return a
}

func (a *Adapter) valuesAsJSON() (s string) {
	var b []byte
	b, _ = json.Marshal(a.keysAndValues)
	s = string(b)
	return s
}
