package main

import "github.com/rs/zerolog/log"

func panicErr(err error) {
	if err != nil {
		log.Fatal().Err(err).Msg("error")
	}
}
