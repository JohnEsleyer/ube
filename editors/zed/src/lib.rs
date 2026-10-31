use zed_extension_api::{self as zed, LanguageServerId, Result};

struct UbeExtension;

impl zed::Extension for UbeExtension {
    fn new() -> Self {
        Self
    }

    fn language_server_command(
        &mut self,
        _language_server_id: &LanguageServerId,
        worktree: &zed::Worktree,
    ) -> Result<zed::Command> {
        // Resolve absolute path to `ube` binary if on PATH, or fallback to direct invocation
        let path = worktree
            .which("ube")
            .unwrap_or_else(|| "ube".to_string());

        Ok(zed::Command {
            command: path,
            args: vec!["lsp".to_string()],
            env: Default::default(),
        })
    }
}

zed::register_extension!(UbeExtension);
