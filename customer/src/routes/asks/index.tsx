import { createFileRoute } from '@tanstack/react-router';
import { AsksPage } from '../../asks/AsksPage';

export const Route = createFileRoute('/asks/')({ component: AsksPage });
