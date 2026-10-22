// Lint fixture: linted as if it were customer/src/sibling-import.ts. Both imports must fail.
import businessPkg from '../../business/package.json';
import { something } from '@todayz/business';

export const leaked = [businessPkg, something];
