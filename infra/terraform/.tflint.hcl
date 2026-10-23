# tflint: the bundled terraform ruleset only (no plugin download needed offline).
# The AWS ruleset is added in Wave 5 with a pinned version and a pre-fetched plugin dir.
plugin "terraform" {
  enabled = true
  preset  = "recommended"
}
