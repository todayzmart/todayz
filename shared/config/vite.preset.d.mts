import type { PreviewOptions, ServerOptions } from 'vite';

export declare const ALLOWED_HOSTS: string[];
export declare function todayzServer(opts: { port: number; hostname: string }): {
  server: ServerOptions;
  preview: PreviewOptions;
};
export declare const intlNoParserAlias: { find: RegExp; replacement: string }[];
export default todayzServer;
