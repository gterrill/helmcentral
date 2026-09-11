package main

// defaultAssistantModel is the OpenRouter model id the onboard assistant
// uses when settings.yaml carries no model, or an explicitly blank one
// (ADR 0093). It must support tool calling: the assistant's whole value is
// the agentic loop over find_places/get_wind_forecast/get_tides, and a model
// that cannot call tools fails every request with "No endpoints found that
// support tool use" rather than answering.
const defaultAssistantModel = "anthropic/claude-sonnet-4.5"
