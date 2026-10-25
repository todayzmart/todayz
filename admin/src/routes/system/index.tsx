import { createFileRoute } from '@tanstack/react-router';
import { SystemPage } from '../../system/SystemPage';

export const Route = createFileRoute('/system/')({ component: SystemPage });
