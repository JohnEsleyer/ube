use zed_extension_api::{self as zed, Command, LanguageServerId, Result, Worktree};

struct UbeExtension;

impl zed::Extension for UbeExtension {
    fn new() -> Self {
        Self
    }

    fn language_server_command(
        &mut self,
        language_server_id: &LanguageServerId,
        worktree: &Worktree,
    ) -> Result<Command> {
        // If the user specified a custom binary in Zed settings, respect it.
        if let Ok(lsp_settings) =
            zed::settings::LspSettings::for_worktree(language_server_id.as_ref(), worktree)
        {
            if let Some(binary) = lsp_settings.binary {
                return Ok(Command {
                    command: binary.path.unwrap_or_else(|| "ube".to_string()),
                    args: binary.arguments.unwrap_or_else(|| vec!["lsp".to_string()]),
                    env: binary.env.unwrap_or_default().into_iter().collect(),
                });
            }
        }

        // Search for `ube` (or fallback to `ubec`) in the worktree / system PATH
        let path = worktree
            .which("ube")
            .or_else(|| worktree.which("ubec"))
            .unwrap_or_else(|| "ube".to_string());

        Ok(Command {
            command: path,
            args: vec!["lsp".to_string()],
            env: Default::default(),
        })
    }
}

zed::register_extension!(UbeExtension);
