/*
 * `vite build` empties dist/, which takes the placeholder with it. That
 * placeholder is what keeps the `//go:embed all:dist` directive in
 * ../embed.go satisfiable in a checkout where the console has never been
 * built - without it `go build ./...` fails for anyone without Node. Put it
 * back after every build.
 */
import { writeFileSync } from 'node:fs'

writeFileSync(new URL('../dist/.gitkeep', import.meta.url), '')
