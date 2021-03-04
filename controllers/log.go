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

package controllers

import (
	"os"
	"time"

	"github.com/AbsaOSS/k8gb/controllers/depresolver"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

type LogFactory struct {
	logger depresolver.Logger
}

func NewLogFactory(config depresolver.Config) *LogFactory {
	return &LogFactory{logger: config.Logger}
}

func (l *LogFactory) Get() zerolog.Logger {
	var logger zerolog.Logger
	var dt = time.RFC822Z
	// We can retrieve stack in case of pkg/errors
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zerolog.SetGlobalLevel(l.logger.Level)
	if l.logger.Level <= zerolog.DebugLevel {
		dt = "15:04:05"
	}
	switch l.logger.OutputFormat {
	case depresolver.JSONFormat:
		// JSON time format as seconds timestamp
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
		// shortening field names timestamp=>t, level=>l , message=>m, caller=>c
		zerolog.TimestampFieldName = "t"
		zerolog.LevelFieldName = "l"
		zerolog.MessageFieldName = "m"
		zerolog.CallerFieldName = "c"
		logger = zerolog.New(os.Stdout).
			With().
			Caller().
			Timestamp().
			Logger()
	case depresolver.ConsoleMonoFormat:
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: dt, NoColor: true}).
			With().
			Caller().
			Timestamp().
			Logger()
	case depresolver.ConsoleColoredFormat:
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: dt, NoColor: false}).
			With().
			Caller().
			Timestamp().
			Logger()
	}
	logger.Info().Msg("Logger configured")
	logger.Debug().Msgf("Logger settings: [%s, %s]", l.logger.OutputFormat, l.logger.Level)
	return logger
}
