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
	"sync"

	"github.com/AbsaOSS/k8gb/controllers/depresolver"
	"github.com/rs/zerolog"
)

var (
	once sync.Once
	log  zerolog.Logger
)

// Logger public static logger, providing instance of initialised logger
func Logger() *zerolog.Logger {
	return &log
}

// Init always initialise logger, no mif config is nil or not
func Init(c *depresolver.Config) {
	once.Do(func() {
		log = newLogger(c).get()
	})
}
