"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.activate = activate;
exports.deactivate = deactivate;
const vscode = require("vscode");
const node_1 = require("vscode-languageclient/node");
let client;
function getCompilerPath() {
    const config = vscode.workspace.getConfiguration('ube');
    return config.get('compilerPath') || 'ube';
}
function startLspClient(context) {
    const config = vscode.workspace.getConfiguration('ube');
    if (!config.get('lsp.enable', true)) {
        return;
    }
    const command = getCompilerPath();
    const serverOptions = {
        command: command,
        args: ['lsp'],
        options: {
            shell: false
        }
    };
    const clientOptions = {
        documentSelector: [{ scheme: 'file', language: 'ube' }],
        synchronize: {
            fileEvents: vscode.workspace.createFileSystemWatcher('**/*.ube')
        }
    };
    client = new node_1.LanguageClient('ubeLSP', 'Ube Language Server', serverOptions, clientOptions);
    client.start().catch((err) => {
        vscode.window.showWarningMessage(`Failed to start Ube language server ('${command} lsp'): ${err.message}. Make sure 'ube' is installed and in your PATH.`);
    });
}
function activate(context) {
    startLspClient(context);
    context.subscriptions.push(vscode.commands.registerCommand('ube.restartServer', async () => {
        if (client) {
            await client.stop();
            client = undefined;
        }
        startLspClient(context);
        vscode.window.showInformationMessage('Ube language server restarted.');
    }));
    context.subscriptions.push(vscode.commands.registerCommand('ube.runFile', () => {
        const editor = vscode.window.activeTextEditor;
        if (!editor || editor.document.languageId !== 'ube') {
            vscode.window.showErrorMessage('No active Ube file to run.');
            return;
        }
        const filePath = editor.document.fileName;
        const terminal = vscode.window.activeTerminal || vscode.window.createTerminal('Ube');
        terminal.show();
        terminal.sendText(`${getCompilerPath()} run "${filePath}"`);
    }));
    context.subscriptions.push(vscode.commands.registerCommand('ube.checkFile', () => {
        const editor = vscode.window.activeTextEditor;
        if (!editor || editor.document.languageId !== 'ube') {
            vscode.window.showErrorMessage('No active Ube file to check.');
            return;
        }
        const filePath = editor.document.fileName;
        const terminal = vscode.window.activeTerminal || vscode.window.createTerminal('Ube Check');
        terminal.show();
        terminal.sendText(`${getCompilerPath()} check "${filePath}"`);
    }));
    context.subscriptions.push(vscode.commands.registerCommand('ube.formatFile', async () => {
        const editor = vscode.window.activeTextEditor;
        if (!editor || editor.document.languageId !== 'ube') {
            return;
        }
        await editor.document.save();
        const filePath = editor.document.fileName;
        const terminal = vscode.window.activeTerminal || vscode.window.createTerminal('Ube Fmt');
        terminal.show();
        terminal.sendText(`${getCompilerPath()} fmt "${filePath}"`);
    }));
}
function deactivate() {
    if (!client) {
        return undefined;
    }
    return client.stop();
}
//# sourceMappingURL=extension.js.map