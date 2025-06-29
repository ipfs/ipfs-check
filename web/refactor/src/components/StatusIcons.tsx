import { CheckIcon as HeroCheckIcon, XMarkIcon, InformationCircleIcon } from '@heroicons/react/24/outline';

interface IconProps {
  className?: string;
}

export const CheckIcon = ({ className = "w-5 h-5 text-green-500 mr-1" }: IconProps) => (
  <HeroCheckIcon className={className} />
);

export const CrossIcon = ({ className = "w-5 h-5 text-red-500 mr-1" }: IconProps) => (
  <XMarkIcon className={className} />
);

export const InfoIcon = ({ className = "w-5 h-5 text-blue-500 mr-1" }: IconProps) => (
  <InformationCircleIcon className={className} />
); 
