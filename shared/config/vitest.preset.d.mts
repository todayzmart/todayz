export declare function vitestPreset(opts?: { dom?: boolean; setupFiles?: string[] }): {
  test: {
    environment: 'happy-dom' | 'node';
    include: string[];
    exclude: string[];
    setupFiles: string[];
    restoreMocks: boolean;
    unstubEnvs: boolean;
    unstubGlobals: boolean;
    passWithNoTests: boolean;
    watch: boolean;
    reporters: string[];
  };
};
export default vitestPreset;
