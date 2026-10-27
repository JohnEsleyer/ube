import { ExtensionContext, workspace } from 'vscode';
import { LanguageClient, LanguageClientOptions, ServerOptions } from 'vscode-languageclient/node';

let client: LanguageClient | undefined;

export function activate(context: ExtensionContext): void {
  const command = workspace.getConfiguration('ube').get<string>('serverPath', 'ubec');
  const serverOptions: ServerOptions = { command, args: ['lsp'] };
  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: 'file', language: 'ube' }],
    synchronize: { fileEvents: workspace.createFileSystemWatcher('**/*.ube') }
  };
  client = new LanguageClient('ubeLSP', 'Ube Language Server', serverOptions, clientOptions);
  context.subscriptions.push(client);
  void client.start();
}

export function deactivate(): Thenable<void> | undefined {
  return client?.stop();
}
