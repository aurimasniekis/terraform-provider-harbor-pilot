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
