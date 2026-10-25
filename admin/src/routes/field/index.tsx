import { createFileRoute } from '@tanstack/react-router';
import { FieldPage } from '../../field/FieldPage';

export const Route = createFileRoute('/field/')({ component: FieldPage });
