import { createFileRoute } from '@tanstack/react-router';
import { StatsPage } from '../../field/StatsPage';

export const Route = createFileRoute('/field-agents/')({ component: StatsPage });
