import * as vscode from 'vscode';
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
} from 'vscode-languageclient/node';

let client: LanguageClient | undefined;

export function activate(context: vscode.ExtensionContext) {
  const config = vscode.workspace.getConfiguration('ube');
  const compilerPath = config.get<string>('compilerPath') || 'ube';
  const enableLsp = config.get<boolean>('lsp.enable') ?? true;

  if (enableLsp) {
    startClient(compilerPath);
  }

  context.subscriptions.push(
    vscode.commands.registerCommand('ube.restartServer', async () => {
      if (client) {
        await client.stop();
        client = undefined;
      }
      startClient(compilerPath);
      vscode.window.showInformationMessage('Ube language server restarted.');
    }),
    vscode.commands.registerCommand('ube.runFile', () => {
      const activeDoc = vscode.window.activeTextEditor?.document;
      if (!activeDoc || activeDoc.languageId !== 'ube') {
        vscode.window.showWarningMessage('No active Ube file to run.');
        return;
      }
      activeDoc.save();
      const terminal = vscode.window.activeTerminal || vscode.window.createTerminal('Ube');
      terminal.show();
      terminal.sendText(`${compilerPath} run "${activeDoc.fileName}"`);
    }),
    vscode.commands.registerCommand('ube.checkFile', () => {
      const activeDoc = vscode.window.activeTextEditor?.document;
      if (!activeDoc || activeDoc.languageId !== 'ube') {
        vscode.window.showWarningMessage('No active Ube file to check.');
        return;
      }
      activeDoc.save();
      const terminal = vscode.window.activeTerminal || vscode.window.createTerminal('Ube');
      terminal.show();
      terminal.sendText(`${compilerPath} check "${activeDoc.fileName}"`);
    }),
    vscode.commands.registerCommand('ube.formatFile', async () => {
      const activeDoc = vscode.window.activeTextEditor?.document;
      if (!activeDoc || activeDoc.languageId !== 'ube') {
        return;
      }
      await activeDoc.save();
      const terminal = vscode.window.activeTerminal || vscode.window.createTerminal('Ube');
      terminal.show();
      terminal.sendText(`${compilerPath} fmt -w "${activeDoc.fileName}"`);
    })
  );
}

function startClient(compilerPath: string) {
  const serverOptions: ServerOptions = {
    command: compilerPath,
    args: ['lsp'],
  };

  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: 'file', language: 'ube' }],
    synchronize: {
      fileEvents: vscode.workspace.createFileSystemWatcher('**/*.ube'),
    },
  };

  client = new LanguageClient(
    'ubeLanguageServer',
    'Ube Language Server',
    serverOptions,
    clientOptions
  );

  client.start();
}

export function deactivate(): Thenable<void> | undefined {
  if (!client) {
    return undefined;
  }
  return client.stop();
}
