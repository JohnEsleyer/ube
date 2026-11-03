import * as vscode from 'vscode';
import * as path from 'path';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions
} from 'vscode-languageclient/node';

let client: LanguageClient | undefined;

function getCompilerPath(): string {
    const config = vscode.workspace.getConfiguration('ube');
    return config.get<string>('compilerPath') || 'ube';
}

function startLspClient(context: vscode.ExtensionContext) {
    const config = vscode.workspace.getConfiguration('ube');
    if (!config.get<boolean>('lsp.enable', true)) {
        return;
    }

    const command = getCompilerPath();
    const serverOptions: ServerOptions = {
        command: command,
        args: ['lsp'],
        options: {
            shell: false
        }
    };

    const clientOptions: LanguageClientOptions = {
        documentSelector: [{ scheme: 'file', language: 'ube' }],
        synchronize: {
            fileEvents: vscode.workspace.createFileSystemWatcher('**/*.ube')
        }
    };

    client = new LanguageClient(
        'ubeLSP',
        'Ube Language Server',
        serverOptions,
        clientOptions
    );

    client.start().catch((err) => {
        vscode.window.showWarningMessage(
            `Failed to start Ube language server ('${command} lsp'): ${err.message}. Make sure 'ube' is installed and in your PATH.`
        );
    });
}

export function activate(context: vscode.ExtensionContext) {
    startLspClient(context);

    context.subscriptions.push(
        vscode.commands.registerCommand('ube.restartServer', async () => {
            if (client) {
                await client.stop();
                client = undefined;
            }
            startLspClient(context);
            vscode.window.showInformationMessage('Ube language server restarted.');
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('ube.runFile', () => {
            const editor = vscode.window.activeTextEditor;
            if (!editor || editor.document.languageId !== 'ube') {
                vscode.window.showErrorMessage('No active Ube file to run.');
                return;
            }

            const filePath = editor.document.fileName;
            const terminal = vscode.window.activeTerminal || vscode.window.createTerminal('Ube');
            terminal.show();
            terminal.sendText(`${getCompilerPath()} run "${filePath}"`);
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('ube.checkFile', () => {
            const editor = vscode.window.activeTextEditor;
            if (!editor || editor.document.languageId !== 'ube') {
                vscode.window.showErrorMessage('No active Ube file to check.');
                return;
            }

            const filePath = editor.document.fileName;
            const terminal = vscode.window.activeTerminal || vscode.window.createTerminal('Ube Check');
            terminal.show();
            terminal.sendText(`${getCompilerPath()} check "${filePath}"`);
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('ube.formatFile', async () => {
            const editor = vscode.window.activeTextEditor;
            if (!editor || editor.document.languageId !== 'ube') {
                return;
            }
            await editor.document.save();
            const filePath = editor.document.fileName;
            const terminal = vscode.window.activeTerminal || vscode.window.createTerminal('Ube Fmt');
            terminal.show();
            terminal.sendText(`${getCompilerPath()} fmt "${filePath}"`);
        })
    );
}

export function deactivate(): Thenable<void> | undefined {
    if (!client) {
        return undefined;
    }
    return client.stop();
}
