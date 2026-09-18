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

// defaultEmbeddingModel is the OpenRouter model id the document indexer's
// embedding stage (E1b) uses when settings.yaml carries no embedding_model,
// or an explicitly blank one. It is a third setting, separate from both
// defaultAssistantModel and defaultDocumentModel: it runs over every chunk
// of every consented document rather than once per question or once per
// upload, so its cost and latency multiply by however many chunks the
// library holds. A blank embedding model is not a misconfiguration - it
// turns semantic search off entirely, leaving FTS5 as the library's only
// retriever.
const defaultEmbeddingModel = "openai/text-embedding-3-small"

// defaultEmbeddingDimensions is the vector length requested alongside
// defaultEmbeddingModel. text-embedding-3-small's native size is 1536; 512
// is asked for instead because the whole index is scanned in Go on an
// armv7 box, and this model is trained with Matryoshka representation
// learning, so a vector truncated to 512 dimensions is still a usable
// embedding rather than a degraded one.
const defaultEmbeddingDimensions = 512
