package main

import (
	// Register Plugins via side-effects
	_ "rizznet/internal/categories/strategies"
	_ "rizznet/internal/collectors/http"
	_ "rizznet/internal/collectors/telegram"
	_ "rizznet/internal/publishers/stdout"
	_ "rizznet/internal/publishers/github"
)

func main() {
	Execute()
}
