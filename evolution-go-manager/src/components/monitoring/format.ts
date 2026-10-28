import { format, parseISO } from 'date-fns';
import { ptBR } from 'date-fns/locale';

export const nf = (n: number) => n.toLocaleString('pt-BR');

export const dayLabel = (day: string) => format(parseISO(day), 'dd/MM', { locale: ptBR });
export const dayTooltip = (day: string) => format(parseISO(day), "EEEE, dd 'de' MMMM", { locale: ptBR });
