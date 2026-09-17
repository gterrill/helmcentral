package main

// defaultAssistantModel is the OpenRouter model id the onboard assistant
// uses when settings.yaml carries no model, or an explicitly blank one
// (ADR 0093). It must support tool calling: the assistant's whole value is
// the agentic loop over find_places/get_wind_forecast/get_tides, and a model
// that cannot call tools fails every request with "No endpoints found that
// support tool use" rather than answering.
const defaultAssistantModel = "anthropic/claude-sonnet-4.5"

// defaultDocumentModel is the OpenRouter model id the document indexer's
// enrich stage (documents_enrich.go) uses when settings.yaml carries no
// document_model, or an explicitly blank one (ADR 0106). It is deliberately
// a separate setting from defaultAssistantModel: enrichment runs
// automatically on every uploaded document while Mate is on, so it needs a
// model that is vision-capable (for the image OCR branch) and cheap enough
// to run unattended, rather than whichever model the operator picked for
// interactive chat.
const defaultDocumentModel = "google/gemini-2.5-flash"
