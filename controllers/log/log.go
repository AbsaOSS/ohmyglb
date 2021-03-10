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
	"os"
	"time"

	"github.com/AbsaOSS/k8gb/controllers/depresolver"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

// loggerFactory creates new logger
type loggerFactory struct {
	log depresolver.Log
}

func newLogger(config *depresolver.Config) *loggerFactory {
	if config == nil {
		return &loggerFactory{log: depresolver.Log{}}
	}
	return &loggerFactory{log: config.Log}
}

// Get returns new logger even if it doesn't know level or format.
// In such case returns default logger
func (l *loggerFactory) get() zerolog.Logger {
	var logger zerolog.Logger
	var dt = time.RFC822Z
	if l.log.Format == depresolver.NoFormat {
		l.log.Format = depresolver.JSONFormat
	}
	if l.log.Level == zerolog.NoLevel {
		l.log.Level = zerolog.InfoLevel
	}
	// We can retrieve stack in case of pkg/errors
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zerolog.SetGlobalLevel(l.log.Level)
	if l.log.Level <= zerolog.DebugLevel {
		dt = "15:04:05"
	}
	switch l.log.Format {
	case depresolver.JSONFormat:
		// JSONFormat time format as seconds timestamp
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
		// shortening field names timestamp=>@t, level=>@l , message=>@m, caller=>@c
		zerolog.TimestampFieldName = "@t"
		zerolog.LevelFieldName = "@l"
		zerolog.MessageFieldName = "@m"
		zerolog.CallerFieldName = "@c"
		logger = zerolog.New(os.Stdout).
			With().
			Caller().
			Timestamp().
			Logger()
	case depresolver.SimpleFormat:
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: dt, NoColor: l.log.NoColor}).
			With().
			Caller().
			Timestamp().
			Logger()
	}
	logger.Info().Msg("Logger configured")
	logger.Debug().Msgf("Logger settings: [%s, %s]", l.log.Format, l.log.Level)
	return logger
}
