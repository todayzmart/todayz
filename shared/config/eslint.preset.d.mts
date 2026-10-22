import type { Linter } from 'eslint';

export declare const repoRoot: string;
export declare const APP_FOLDERS: string[];
export declare const APP_PACKAGES: string[];
export declare const DEFAULT_IGNORES: string[];
export declare function boundaryZones(folder: string): { target: string; from: string; message: string }[];
export declare function todayzEslint(opts: { pkgDir: string; react?: boolean; ignores?: string[] }): Linter.Config[];
export default todayzEslint;
