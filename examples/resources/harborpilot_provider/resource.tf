resource "harborpilot_provider" "anthropic" {
  name = "anthropic"
  spec = jsonencode({
    type = "anthropic"
    models = [{
      id                = "claude-sonnet-4-5"
      display_name      = "Claude Sonnet 4.5"
      context_window    = 200000
      max_output_tokens = 64000
      capabilities      = ["tools", "streaming"]
      pricing = {
        currency        = "USD"
        input_per_mtok  = "3"
        output_per_mtok = "15"
        price_version   = "2026-11"
      }
    }]
  })
}

# A local model server (vLLM, llama.cpp, LM Studio) on a private address.
resource "harborpilot_provider" "local" {
  name = "local-vllm"
  spec = jsonencode({
    type                  = "openai_compatible"
    base_url              = "http://10.0.0.5:8000/v1"
    allow_private_network = true
    models                = [{ id = "qwen3-coder", display_name = "Qwen3 Coder", context_window = 131072, max_output_tokens = 16384, capabilities = ["tools", "streaming"] }]
  })
}

# An OpenAI-compatible router: pin a model to one upstream provider and set
# extra request fields (Harbor Pilot 0.1.6 or later). Fields left out of a
# model are cleared on apply.
resource "harborpilot_provider" "router" {
  name = "router"
  spec = jsonencode({
    type         = "openai_compatible"
    display_name = "Router"
    base_url     = "https://router.example.com/v1"
    models = [{
      id = "vendor/model-a"
      provider_routing = {
        only            = ["provider-a"]
        allow_fallbacks = false
        data_collection = "deny"
      }
      request_fields       = { temperature = 0.2, transforms = ["middle-out"] }
      request_field_policy = "configured"
    }]
  })
}

# A reasoning model behind an OpenAI-compatible router: follow the client's
# reasoning settings in the router's style, with medium as the default level
# (Harbor Pilot 0.1.7 or later). An omitted reasoning option is cleared on apply.
resource "harborpilot_provider" "reasoning_router" {
  name = "reasoning-router"
  spec = jsonencode({
    type         = "openai_compatible"
    display_name = "Reasoning router"
    base_url     = "https://router.example.com/v1"
    models = [{
      id = "vendor/reasoning-model"
      reasoning = {
        mode    = "client"
        style   = "openrouter_reasoning"
        effort  = "medium"
        efforts = ["low", "medium", "high"]
      }
    }]
  })
}
