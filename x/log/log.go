package log

import (
	"github.com/rs/zerolog"
	"os"
)

var (
	Logger = zerolog.New(os.Stderr).With().Timestamp().Stack().Logger()
)
